package infrastructure

import (
	"bytes"
	"strings"
	"testing"
)

func TestOTPPageEscapesName(t *testing.T) {
	var b bytes.Buffer
	if err := otpPage.Execute(&b, map[string]any{"Heading": "H", "Name": "<script>x</script>", "Lead": "l", "Code": "123456", "Minutes": 10}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "<script>") || !strings.Contains(out, "123456") {
		t.Fatalf("unexpected page: %s", out)
	}
}
