package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDoMultipart_encodesDataAndFiles verifies the compliance create/update wire
// format: a "data" form field carrying the JSON payload plus one file part per
// upload, sent as multipart/form-data with Basic auth, and the JSON response
// decoded into out.
func TestDoMultipart_encodesDataAndFiles(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "passport.pdf")
	if err := os.WriteFile(fpath, []byte("PDFBYTES"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotData, gotFileField, gotFileName, gotContentType, gotAuthUser string
	var gotFile []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotAuthUser, _, _ = r.BasicAuth()
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("request is not multipart: %v", err)
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("read part: %v", err)
				return
			}
			switch p.FormName() {
			case "data":
				b, _ := io.ReadAll(p)
				gotData = string(b)
			case "documents[0].file":
				gotFileField = p.FormName()
				gotFileName = p.FileName()
				gotFile, _ = io.ReadAll(p)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"compliance_id":"CMP123","message":"submitted"}`))
	}))
	defer srv.Close()

	c := New("MAEXAMPLEAUTHID00000", "secret-token", 5*time.Second)
	var out struct {
		ComplianceID string `json:"compliance_id"`
		Message      string `json:"message"`
	}
	apiErr, err := c.DoMultipart("POST", srv.URL+"/PhoneNumber/Compliance/",
		[]byte(`{"country_iso":"US","number_type":"local"}`),
		map[string]string{"documents[0].file": fpath}, &out)
	if err != nil {
		t.Fatalf("DoMultipart returned error: %v", err)
	}
	if apiErr != nil {
		t.Fatalf("DoMultipart returned API error: %v", apiErr)
	}

	if gotData != `{"country_iso":"US","number_type":"local"}` {
		t.Errorf("data part = %q", gotData)
	}
	if string(gotFile) != "PDFBYTES" {
		t.Errorf("uploaded file content = %q, want PDFBYTES", gotFile)
	}
	if gotFileField != "documents[0].file" {
		t.Errorf("file field name = %q", gotFileField)
	}
	if gotFileName != "passport.pdf" {
		t.Errorf("uploaded file name = %q, want passport.pdf", gotFileName)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotAuthUser != "MAEXAMPLEAUTHID00000" {
		t.Errorf("Basic auth user = %q", gotAuthUser)
	}
	if out.ComplianceID != "CMP123" || out.Message != "submitted" {
		t.Errorf("decoded response = %+v", out)
	}
}

// TestDoMultipart_dryRunSkipsRequest confirms dry-run prints without sending and
// without even opening the (here nonexistent) file.
func TestDoMultipart_dryRunSkipsRequest(t *testing.T) {
	c := New("MA", "tok", time.Second)
	c.DryRun = true
	apiErr, err := c.DoMultipart("POST", "https://api.plivo.com/v1/x",
		[]byte(`{"a":1}`), map[string]string{"documents[0].file": "/no/such/file.pdf"}, nil)
	if err != nil || apiErr != nil {
		t.Fatalf("dry-run should be a no-op, got err=%v apiErr=%v", err, apiErr)
	}
}
