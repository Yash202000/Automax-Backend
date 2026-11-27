package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/automax/backend/internal/config"
)

// The AD-Baseer API log must show each call with status and timing while
// masking credentials, tokens, the OTP and employee details.
func TestADBaseerAPILogMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case adBaseerTokenPath:
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "tok-SECRET-123", "authorizedModules": []string{"AD"}})
		case adBaseerValidatePath:
			json.NewEncoder(w).Encode(map[string]any{"isValid": true, "otp": 0, "requestID": "req-42"})
		case adBaseerIsValidPath:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"isValid": false, "detailedCodeID": 1002, "businessErrorMessage": "Invalid OTP"})
		}
	}))
	defer srv.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	client, err := NewADBaseerClient(config.ADBaseerConfig{
		Mode: "mock", BaseURL: srv.URL, Service: "svc-SECRET", Key: "key-SECRET", ModuleID: "7",
		TimeoutSeconds: 5, OTPTTLSeconds: 300, OTPMaxAttempts: 3, LogAPI: true,
	}, "development")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := client.StartLogin(context.Background(), "jdoe", "Pa$$w0rd-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if ch.AccessToken != "tok-SECRET-123" || ch.RequestID != "req-42" {
		t.Fatalf("challenge changed: %+v", ch)
	}
	if _, err := client.VerifyOTP(context.Background(), ch, "998877"); err == nil {
		t.Fatal("expected invalid OTP")
	}

	out := buf.String()
	t.Log("\n" + out)
	for _, secret := range []string{"svc-SECRET", "key-SECRET", "Pa$$w0rd-SECRET", "tok-SECRET-123", "998877"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{
		"request  POST " + srv.URL + adBaseerTokenPath + " step=GetToken bearer=no",
		"step=ValidateCredentialsOTP bearer=yes",
		"response POST " + srv.URL + adBaseerIsValidPath + " step=IsValidOTP status=400",
		`"requestID":"req-42"`, `"Email":"jdoe"`, `"businessErrorMessage":"Invalid OTP"`,
		`StartLogin (GetToken + ValidateCredentialsOTP) user="jdoe"`, "outcome=success",
		"VerifyOTP (IsValidOTP)", "outcome=failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "took=") {
		t.Errorf("no timing in log")
	}
}

func TestADBaseerAPILogRedactsEmployeeInfo(t *testing.T) {
	c := &adBaseerClient{cfg: config.ADBaseerConfig{LogAPI: true, Key: "k"}}
	got := c.logBody([]byte(`{"isValid":true,"employeeInfo":{"fullNameEn":"John Doe","email":"j@x.sa","status":1,"employeeNumber":"123"}}`))
	for _, leak := range []string{"John Doe", `"123"`} {
		if strings.Contains(got, leak) {
			t.Errorf("personal data leaked: %s", got)
		}
	}
	if !strings.Contains(got, `"email":"j@x.sa"`) || !strings.Contains(got, `"status":1`) {
		t.Errorf("kept fields missing: %s", got)
	}
}
