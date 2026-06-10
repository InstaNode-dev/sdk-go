package instant

// client_helpers_test.go — pins the marshal-error branches of the putJSON /
// patchJSON helpers introduced for the operate verbs. No public method can
// reach them (every operate body is a plain struct), so they are exercised
// directly with an unmarshalable body.

import (
	"context"
	"strings"
	"testing"
)

func TestPutJSON_MarshalError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	// A func value is not JSON-serializable — json.Marshal must fail before
	// any network I/O happens.
	err := c.putJSON(context.Background(), "/x", func() {}, nil)
	if err == nil || !strings.Contains(err.Error(), "marshalling request body") {
		t.Errorf("err = %v, want marshalling error", err)
	}
}

func TestPatchJSON_MarshalError(t *testing.T) {
	c := New(WithBaseURL("http://127.0.0.1:0"))
	err := c.patchJSON(context.Background(), "/x", func() {}, nil)
	if err == nil || !strings.Contains(err.Error(), "marshalling request body") {
		t.Errorf("err = %v, want marshalling error", err)
	}
}
