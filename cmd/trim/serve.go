package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/vinodhalaharvi/trim/engine"
	"github.com/vinodhalaharvi/trim/media"
	"github.com/vinodhalaharvi/trim/web"
)

type server struct {
	in    *input
	mu    sync.Mutex
	cache map[string]*engine.Solution // by pin set
	order []string
}

func (s *server) solution(pins map[int]bool) *engine.Solution {
	var ids []string
	for i := range pins {
		ids = append(ids, strconv.Itoa(i))
	}
	sort.Strings(ids)
	key := strings.Join(ids, ",")
	s.mu.Lock()
	defer s.mu.Unlock()
	if sol, ok := s.cache[key]; ok {
		return sol
	}
	// one solve answers every length: capacity is the whole talk
	full := s.in.Talk.MakeCut(allOf(len(s.in.Sentences))).Length + 2*engine.Pad*float64(len(s.in.Sentences))
	sol := s.in.Talk.Solve(full, pins)
	s.cache[key] = sol
	s.order = append(s.order, key)
	if len(s.order) > 8 {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
	return sol
}

func parsePins(q string, n int) map[int]bool {
	pins := map[int]bool{}
	for _, p := range strings.Split(q, ",") {
		if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && i >= 0 && i < n {
			pins[i] = true
		}
	}
	return pins
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func serve(args []string) error {
	fl := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fl.Int("port", 7777, "")
	noOpen := fl.Bool("no-open", false, "")
	scores := fl.String("scores", "", "")
	pos, err := parseInterleaved(fl, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 || len(pos) > 2 {
		return errors.New("usage: trim serve video [subtitles]")
	}
	subsPath := ""
	if len(pos) == 2 {
		subsPath = pos[1]
	}
	in, err := load(pos[0], subsPath, *scores, true)
	if err != nil {
		return err
	}
	s := &server{in: in, cache: map[string]*engine.Solution{}}
	go s.solution(nil) // warm up while the browser opens

	mux := http.NewServeMux()
	sub, _ := fs.Sub(web.FS, ".")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/media", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, in.Media) })
	mux.HandleFunc("/api/talk", func(w http.ResponseWriter, r *http.Request) {
		type sent struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
			Score float64 `json:"score"`
		}
		var ss []sent
		for _, x := range in.Talk.Sentences {
			ss = append(ss, sent{x.Start, x.End, x.Text, x.Score})
		}
		writeJSON(w, map[string]any{
			"name": filepath.Base(in.Media), "length": in.Info.Length, "hasVideo": in.Info.HasVideo,
			"sentences": ss, "talkInterest": in.Talk.TalkInterest(),
		})
	})
	mux.HandleFunc("/api/cut", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		target, err := strconv.ParseFloat(q.Get("target"), 64)
		if err != nil || target <= 0 {
			http.Error(w, "bad target", http.StatusBadRequest)
			return
		}
		k, _ := strconv.Atoi(q.Get("k"))
		pins := parsePins(q.Get("pins"), len(in.Sentences))
		sol := s.solution(pins)
		cuts := sol.Best(target, max(1, min(k, 3)))
		if cuts == nil {
			cuts = []engine.Cut{}
		}
		writeJSON(w, map[string]any{"cuts": cuts, "minTarget": math.Ceil(math.Max(in.Talk.PinnedLength(pins), sol.MinLength()))})
	})
	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Sentences []int   `json:"sentences"`
			Target    float64 `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Sentences) == 0 {
			http.Error(w, "nothing to export", http.StatusBadRequest)
			return
		}
		for _, i := range req.Sentences {
			if i < 0 || i >= len(in.Sentences) {
				http.Error(w, "bad sentence", http.StatusBadRequest)
				return
			}
		}
		c := in.Talk.MakeCut(req.Sentences)
		name := freeName(outputName("", in.Media, req.Target, 0, 1))
		if err := media.Render(in.Media, name, in.Info, c.Spans); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		srt := strings.TrimSuffix(name, filepath.Ext(name)) + ".srt"
		_ = media.WriteSRTFile(srt, media.Retime(in.Sentences, c.Sentences, media.Snap(c.Spans, in.Info.FPS)))
		abs, _ := filepath.Abs(name)
		length := 0.0
		for _, sp := range media.Snap(c.Spans, in.Info.FPS) {
			length += sp[1] - sp[0]
		}
		writeJSON(w, map[string]any{"path": abs, "length": length})
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Printf("editing %s at %s (Ctrl-C to stop)\n", filepath.Base(in.Media), url)
	if !*noOpen {
		openBrowser(url)
	}
	return http.Serve(ln, mux)
}

// freeName adds -2, -3, … so an export never overwrites an earlier one.
func freeName(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for k := 2; ; k++ {
		q := fmt.Sprintf("%s-%d%s", base, k, ext)
		if _, err := os.Stat(q); err != nil {
			return q
		}
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
