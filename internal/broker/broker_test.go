package broker

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		filter, topic string
		want          bool
	}{
		{"rocateq/store/+/controller/+/telemetry", "rocateq/store/NL-1/controller/C1/telemetry", true},
		{"rocateq/store/+/controller/+/telemetry", "rocateq/store/NL-1/controller/C1/events", false},
		{"rocateq/store/NL-1/#", "rocateq/store/NL-1/controller/C1/events", true},
		{"rocateq/store/NL-1/#", "rocateq/store/NL-2/controller/C1/events", false},
		{"rocateq/+", "rocateq/store/x", false},
	}
	for _, c := range cases {
		if got := Match(c.filter, c.topic); got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.filter, c.topic, got, c.want)
		}
	}
}

func TestParseTopic(t *testing.T) {
	s, c, k, ok := ParseTopic(Topic("NL-1", "C1", KindCommands))
	if !ok || s != "NL-1" || c != "C1" || k != KindCommands {
		t.Fatalf("got %q %q %q %v", s, c, k, ok)
	}
}
