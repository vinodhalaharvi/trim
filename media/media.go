// Package media probes and cuts audio and video with ffmpeg.
package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vinodhalaharvi/trim/subs"
)

// ErrNoFFmpeg explains how to get ffmpeg.
var ErrNoFFmpeg = errors.New("ffmpeg not found: install it (brew install ffmpeg, apt install ffmpeg) or use -dry-run")

// Info describes an input file.
type Info struct {
	Length   float64
	HasVideo bool
	HasAudio bool
	FPS      float64 // video frame rate; 0 for audio
}

// Probe reads the length and streams of a media file with ffprobe.
func Probe(path string) (Info, error) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return Info{}, ErrNoFFmpeg
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration:stream=codec_type,avg_frame_rate", "-of", "json", path).Output()
	if err != nil {
		return Info{}, fmt.Errorf("%s: ffprobe could not read it", path)
	}
	var p struct {
		Format  struct{ Duration string } `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Rate      string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, err
	}
	info := Info{}
	info.Length, _ = strconv.ParseFloat(p.Format.Duration, 64)
	for _, s := range p.Streams {
		if s.CodecType == "video" && !info.HasVideo {
			info.HasVideo = true
			if a, b, ok := strings.Cut(s.Rate, "/"); ok {
				n, _ := strconv.ParseFloat(a, 64)
				d, _ := strconv.ParseFloat(b, 64)
				if d > 0 {
					info.FPS = n / d
				}
			}
		}
		info.HasAudio = info.HasAudio || s.CodecType == "audio"
	}
	if !info.HasVideo && !info.HasAudio {
		return info, fmt.Errorf("%s: no audio or video stream", path)
	}
	return info, nil
}

const fade = 0.03 // seconds of audio fade at each cut, to avoid clicks

// batch is how many spans one ffmpeg run decodes at once; each open input
// costs memory, so long edit lists are rendered in batches and then joined.
const batch = 6

// Render writes the spans of in, joined in order, to out. Video is
// re-encoded (H.264) so cuts land exactly on frames; audio fades briefly at
// each cut.
func Render(in, out string, info Info, spans [][2]float64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrNoFFmpeg
	}
	spans = Snap(spans, info.FPS)
	if len(spans) == 0 {
		return errors.New("nothing to render")
	}
	if len(spans) <= batch {
		return renderBatch(in, out, out, info, spans)
	}
	dir, err := os.MkdirTemp("", "trim-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var list strings.Builder
	for k := 0; k < len(spans); k += batch {
		part := filepath.Join(dir, fmt.Sprintf("part%04d.mkv", k/batch))
		if err := renderBatch(in, part, out, info, spans[k:min(k+batch, len(spans))]); err != nil {
			return err
		}
		fmt.Fprintf(&list, "file '%s'\n", part)
	}
	listFile := filepath.Join(dir, "parts.txt")
	if err := os.WriteFile(listFile, []byte(list.String()), 0o644); err != nil {
		return err
	}
	// every part starts on a fresh keyframe, so they join without re-encoding
	// Parts start on fresh keyframes, so video joins without re-encoding.
	// Parts carry uncompressed audio, encoded once here: encoding each part
	// would add encoder padding at every join and drift against the picture.
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c:v", "copy"}
	if info.HasAudio {
		if codec := audioCodec(out); codec != "" {
			args = append(args, "-c:a", codec)
		}
	}
	return run(append(append(args, container(out)...), out))
}

// renderBatch cuts a few spans into one file. Codecs follow final, the name
// of the file the user asked for, even when writing an intermediate part.
func renderBatch(in, out, final string, info Info, spans [][2]float64) error {
	// Each span is its own seeked input, so ffmpeg decodes only what is kept.
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	var g, labels strings.Builder
	for k, sp := range spans {
		a, b := sp[0], sp[1]
		args = append(args, "-ss", fmt.Sprintf("%.3f", a), "-t", fmt.Sprintf("%.3f", b-a), "-i", in)
		if info.HasVideo {
			fmt.Fprintf(&g, "[%d:v]setpts=PTS-STARTPTS[v%d];", k, k)
			fmt.Fprintf(&labels, "[v%d]", k)
		}
		if info.HasAudio {
			fmt.Fprintf(&g, "[%d:a]asetpts=PTS-STARTPTS,afade=t=in:st=0:d=%.3f,afade=t=out:st=%.3f:d=%.3f[a%d];",
				k, fade, max(0, b-a-fade), fade, k)
			fmt.Fprintf(&labels, "[a%d]", k)
		}
	}
	v, a := 0, 0
	if info.HasVideo {
		v = 1
	}
	if info.HasAudio {
		a = 1
	}
	fmt.Fprintf(&g, "%sconcat=n=%d:v=%d:a=%d%s", labels.String(), len(spans), v, a, outs(info))
	script, err := os.CreateTemp("", "trim-*.filter")
	if err != nil {
		return err
	}
	defer os.Remove(script.Name())
	script.WriteString(g.String())
	script.Close()
	args = append(args, "-filter_complex_script", script.Name())
	if info.HasVideo {
		args = append(args, "-map", "[v]", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p")
	}
	if info.HasAudio {
		args = append(args, "-map", "[a]")
		if out != final {
			args = append(args, "-c:a", "pcm_s16le")
		} else if codec := audioCodec(final); codec != "" {
			args = append(args, "-c:a", codec)
		}
	}
	if out == final {
		args = append(args, container(out)...)
	}
	return run(append(args, out))
}

func container(out string) []string {
	switch strings.ToLower(filepath.Ext(out)) {
	case ".mp4", ".m4a", ".mov":
		return []string{"-movflags", "+faststart"}
	}
	return nil
}

func run(args []string) error {
	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg failed: %s", msg)
	}
	return nil
}

// Snap moves cut points onto the video's frame grid. Video can only be cut
// between frames while audio is cut to the sample; without snapping, every
// cut would shift the audio against the picture a little more.
func Snap(spans [][2]float64, fps float64) [][2]float64 {
	if fps <= 0 {
		return spans
	}
	out := make([][2]float64, 0, len(spans))
	for _, sp := range spans {
		// snap inward, so a cut never grows past the length it was chosen for
		a, b := math.Ceil(sp[0]*fps-1e-6)/fps, math.Floor(sp[1]*fps+1e-6)/fps
		if b > a {
			out = append(out, [2]float64{a, b})
		}
	}
	return out
}

func outs(info Info) string {
	s := ""
	if info.HasVideo {
		s += "[v]"
	}
	if info.HasAudio {
		s += "[a]"
	}
	return s
}

func audioCodec(out string) string {
	switch strings.ToLower(filepath.Ext(out)) {
	case ".mp3":
		return "libmp3lame"
	case ".wav":
		return "pcm_s16le"
	case ".ogg", ".opus":
		return "libopus"
	case ".flac":
		return "flac"
	}
	return "aac"
}

// Retime maps the cues of the kept sentences onto the output's timeline.
func Retime(sentences []subs.Sentence, kept []int, spans [][2]float64) []subs.Cue {
	var out []subs.Cue
	for _, i := range kept {
		for _, c := range sentences[i].Cues {
			off := 0.0
			for _, sp := range spans {
				if c.Start >= sp[0]-1e-6 && c.Start < sp[1] {
					out = append(out, subs.Cue{Start: off + c.Start - sp[0], End: off + min(c.End, sp[1]) - sp[0], Text: c.Text})
					break
				}
				off += sp[1] - sp[0]
			}
		}
	}
	return out
}

// WriteSRTFile writes cues to path.
func WriteSRTFile(path string, cues []subs.Cue) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := subs.WriteSRT(f, cues); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
