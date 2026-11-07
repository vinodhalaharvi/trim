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

// Render writes the spans of in, joined in order, to out. Video is
// re-encoded (H.264) so cuts land exactly; audio fades briefly at each cut.
func Render(in, out string, info Info, spans [][2]float64) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrNoFFmpeg
	}
	if len(spans) == 0 {
		return errors.New("nothing to render")
	}
	var g strings.Builder
	var labels strings.Builder
	for k, sp := range Snap(spans, info.FPS) {
		a, b := sp[0], sp[1]
		if info.HasVideo {
			fmt.Fprintf(&g, "[0:v]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v%d];", a, b, k)
			fmt.Fprintf(&labels, "[v%d]", k)
		}
		if info.HasAudio {
			d := b - a
			fmt.Fprintf(&g, "[0:a]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS,afade=t=in:st=0:d=%.3f,afade=t=out:st=%.3f:d=%.3f[a%d];",
				a, b, fade, max(0, d-fade), fade, k)
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
	fmt.Fprintf(&g, "%sconcat=n=%d:v=%d:a=%d", labels.String(), len(spans), v, a)
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", in, "-filter_complex", g.String() + outs(info)}
	if info.HasVideo {
		args = append(args, "-map", "[v]", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p")
	}
	if info.HasAudio {
		args = append(args, "-map", "[a]")
		if codec := audioCodec(out); codec != "" {
			args = append(args, "-c:a", codec)
		}
	}
	if strings.EqualFold(filepath.Ext(out), ".mp4") || strings.EqualFold(filepath.Ext(out), ".m4a") || strings.EqualFold(filepath.Ext(out), ".mov") {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, out)
	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
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
		a, b := math.Round(sp[0]*fps)/fps, math.Round(sp[1]*fps)/fps
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
