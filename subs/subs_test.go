package subs

import (
	"strings"
	"testing"
)

func TestSRTAndVTT(t *testing.T) {
	srt := "\ufeff1\n00:00:01,000 --> 00:00:03,500\nHello <i>there</i>,\n\n2\n00:00:03,500 --> 00:00:06,000\nthis is one sentence.\n\n3\n00:00:09,000 --> 00:00:10,000\nAnd another\n"
	vtt := "WEBVTT\n\nintro\n00:01.000 --> 00:03.500 align:start\nHello there,\n\n00:03.500 --> 00:06.000\nthis is one sentence.\n\n00:09.000 --> 00:10.000\nAnd another\n"
	for name, in := range map[string]string{"srt": srt, "vtt": vtt} {
		cues, err := Read(strings.NewReader(in))
		if err != nil || len(cues) != 3 {
			t.Fatalf("%s: %v, %d cues", name, err, len(cues))
		}
		ss := Sentences(cues, 1.0, 25)
		if len(ss) != 2 || ss[0].Text != "Hello there, this is one sentence." || ss[0].Start != 1 || ss[0].End != 6 {
			t.Fatalf("%s: grouped wrong: %+v", name, ss)
		}
	}
}

func TestTimes(t *testing.T) {
	for in, want := range map[string]float64{"01:02:03,456": 3723.456, "02:03.5": 123.5, "0:12": 12} {
		if got, err := ParseTime(in); err != nil || got != want {
			t.Errorf("%s: got %v %v", in, got, err)
		}
	}
	if FormatTime(3723.456) != "01:02:03,456" {
		t.Error(FormatTime(3723.456))
	}
}
