package core

import "testing"

func TestValidateTopic(t *testing.T) {
	ok := []string{
		"pr/octocat/hello/1",
		"build/ci/42",
		"deploy/prod/api-service",
		"job/abc_123",
		"chat/room.general",
	}
	bad := []string{
		"", "/", "pr", "pr/", "/x", "pr//1", "pr/hello world/1", "pr/x?y/1",
	}
	for _, s := range ok {
		if err := ValidateTopic(s); err != nil {
			t.Errorf("expected %q valid, got %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateTopic(s); err == nil {
			t.Errorf("expected %q invalid", s)
		}
	}
}

func TestParseObjectType(t *testing.T) {
	cases := map[string]string{
		"pr/octocat/hello/1": "pr",
		"build/ci/42":        "build",
	}
	for topic, want := range cases {
		got, err := ParseObjectType(topic)
		if err != nil || got != want {
			t.Errorf("ParseObjectType(%q) = %q,%v; want %q,nil", topic, got, err, want)
		}
	}
	if _, err := ParseObjectType(""); err == nil {
		t.Error("expected error on empty topic")
	}
}
