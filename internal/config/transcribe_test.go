package config

import "testing"

func TestCaptionsConfig(t *testing.T) {
	c, err := LoadFrom(get(map[string]string{}))
	if err != nil || c.CaptionsOn() {
		t.Fatalf("off by default: %v %v", c.CaptionsOn(), err)
	}
	c, _ = LoadFrom(get(map[string]string{"TRANSCRIBE_URL": " http://whisper.internal:8080/v1/ ", "TRANSCRIBE_API_KEY": " k ", "TRANSCRIBE_MODEL": "small"}))
	if !c.CaptionsOn() || c.TranscribeURL != "http://whisper.internal:8080/v1" || c.TranscribeKey != "k" || c.TranscribeModel != "small" {
		t.Fatalf("%+v", c)
	}
	c, _ = LoadFrom(get(map[string]string{"TRANSCRIBE_DEMO": "true"}))
	if !c.CaptionsOn() || c.TranscribeURL != "" {
		t.Fatal("demo alone turns captions on with no address")
	}
}
