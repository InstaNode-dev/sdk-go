package instant

import (
	"bytes"
	"strings"
	"testing"
)

// errAfterWriter is an io.Writer that succeeds for the first n Write calls and
// then returns an error on every subsequent call. It lets us drive the
// multipart writer's WriteField / CreateFormFile / Close failure arms in
// writeStackMultipart, which a *bytes.Buffer (whose Write never fails) cannot
// reach.
type errAfterWriter struct {
	remaining int
}

func (w *errAfterWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errWriteFull
	}
	w.remaining--
	return len(p), nil
}

var errWriteFull = &writeErr{}

type writeErr struct{}

func (*writeErr) Error() string { return "writer full" }

// TestWriteStackMultipart_WriterFailures sweeps an increasing write budget and
// asserts that EVERY multipart error arm (manifest field, name field, env
// field, CreateFormFile, io.Copy-into-part, and Close) is reached for some
// budget, and that a generous budget succeeds. Sweeping rather than hard-coding
// per-arm budgets keeps the test robust to the multipart writer's internal
// flush timing (which can shift between Go versions) — we only require that
// each documented failure message is reachable, not that it lands at an exact
// write count.
func TestWriteStackMultipart_WriterFailures(t *testing.T) {
	opts := CreateStackOpts{
		Name: "app",
		Env:  "production",
		Services: []StackServiceSpec{
			{Name: "api", Tarball: bytes.NewBufferString("tar")},
		},
	}

	wantFrags := []string{
		"writing manifest field",
		"writing name field",
		"writing env field",
		"building tarball field",
		"reading tarball",
		"closing multipart writer",
	}
	seen := make(map[string]bool, len(wantFrags))
	sawSuccess := false

	// 64 is comfortably above the ~8 writes a one-service body needs; the upper
	// budgets exercise the success path.
	for budget := 0; budget <= 64; budget++ {
		w := &errAfterWriter{remaining: budget}
		_, err := writeStackMultipart(w, opts)
		if err == nil {
			sawSuccess = true
			continue
		}
		for _, frag := range wantFrags {
			if strings.Contains(err.Error(), frag) {
				seen[frag] = true
			}
		}
	}

	for _, frag := range wantFrags {
		if !seen[frag] {
			t.Errorf("no write budget reached the %q error arm", frag)
		}
	}
	if !sawSuccess {
		t.Error("no write budget produced a successful body")
	}
}

// TestWriteStackMultipart_Success confirms the helper returns a multipart
// Content-Type and writes a non-empty body on the happy path.
func TestWriteStackMultipart_Success(t *testing.T) {
	var buf bytes.Buffer
	ct, err := writeStackMultipart(&buf, CreateStackOpts{
		Name:     "app",
		Services: []StackServiceSpec{{Name: "api", Tarball: bytes.NewBufferString("tar")}},
	})
	if err != nil {
		t.Fatalf("writeStackMultipart: %v", err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data;") {
		t.Errorf("content-type = %q", ct)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty multipart body")
	}
}
