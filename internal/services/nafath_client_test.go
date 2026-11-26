package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/config"
)

// fakeGateway answers each path with a fixed status and body, records the
// last request body per path, and optionally marks responses as simulator.
type fakeGateway struct {
	status map[string]int
	body   map[string]string
	got    map[string]map[string]any
	auth   map[string]string
	mock   bool
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{status: map[string]int{}, body: map[string]string{}, got: map[string]map[string]any{}, auth: map[string]string{}}
}

func (g *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	g.got[r.URL.Path] = m
	g.auth[r.URL.Path] = r.Header.Get("Authorization")
	if g.mock {
		w.Header().Set(nafathMockHeader, "true")
	}
	w.Header().Set("Content-Type", "application/json")
	status := g.status[r.URL.Path]
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, g.body[r.URL.Path])
}

func newTestNafathClient(t *testing.T, g *fakeGateway, mode string) NafathClient {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	c, err := NewNafathClient(config.NafathConfig{
		Mode: mode, BaseURL: srv.URL, Service: "svc", Key: "secret-key", AppID: "13", CheckAppID: "17",
		TimeoutSeconds: 5, PollSeconds: 10,
	}, "development")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const completedBody = `{"errorMessage":null,"checkStatus":"COMPLETED","accessToken":null,"responseJSON":null,
"userInfo":{"id":1111111111,"id_version":3,"full_namear":"أحمد العلي","full_nameen":"AHMED ALALI"},
"person":null,"employeeList":[{"employeeNumber":9100201,"status":1}],"userDelegations":[],
"detailedCodeID":0,"detailedCode":null,"businessErrorMessage":null,"exceptionMessage":null,"itemsCount":0}`

func TestNafathGetTokenSendsDocumentShape(t *testing.T) {
	g := newFakeGateway()
	g.body[nafathTokenPath] = `{"accessToken":"tok","expiryDate":"2026-11-10T12:00:00","errorMessage":null}`
	c := newTestNafathClient(t, g, "mock")
	tok, err := c.GetToken(context.Background(), "1111111111")
	if err != nil || tok != "tok" {
		t.Fatalf("GetToken = %q, %v", tok, err)
	}
	got := g.got[nafathTokenPath]
	if got["Service"] != "svc" || got["Key"] != "secret-key" || got["IdNumber"] != float64(1111111111) {
		t.Fatalf("request body %v: IdNumber must be a JSON number", got)
	}
}

func TestNafathGetTokenErrors(t *testing.T) {
	g := newFakeGateway()
	g.status[nafathTokenPath] = http.StatusUnauthorized
	g.body[nafathTokenPath] = `{"accessToken":null,"expiryDate":"0001-01-01T00:00:00","errorMessage":null}`
	c := newTestNafathClient(t, g, "mock")
	var refusal401 *NafathRefusal
	if _, err := c.GetToken(context.Background(), "1111111111"); !errors.As(err, &refusal401) || refusal401.Message != "401 Unauthorized" {
		t.Fatalf("401 without a message: want Nafath's status \"401 Unauthorized\", got %v", err)
	}
	g.status[nafathTokenPath] = http.StatusBadRequest
	g.body[nafathTokenPath] = `{"accessToken":null,"errorMessage":"Invalid IdNumber"}`
	_, err := c.GetToken(context.Background(), "1111111111")
	var refusal *NafathRefusal
	if !errors.As(err, &refusal) || refusal.Message != "Invalid IdNumber" {
		t.Fatalf("errorMessage must become a refusal, got %v", err)
	}
}

func TestNafathSendRequestBothShapes(t *testing.T) {
	for name, body := range map[string]string{
		"top level":     `{"errorMessage":null,"transID":"aa-bb","random":53,"checkStatus":null}`,
		"transId":       `{"errorMessage":null,"transId":"aa-bb","random":"53"}`,
		"response json": `{"errorMessage":null,"checkStatus":null,"responseJSON":"{\"random\":\"53\",\"transId\":\"aa-bb\"}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			g := newFakeGateway()
			g.body[nafathSendPath] = body
			c := newTestNafathClient(t, g, "mock")
			req, err := c.SendRequest(context.Background(), "tok", "1111111111")
			if err != nil || req.TransID != "aa-bb" || req.Random != "53" {
				t.Fatalf("SendRequest = %+v, %v", req, err)
			}
			got := g.got[nafathSendPath]
			if got["appID"] != float64(13) || got["idNumber"] != float64(1111111111) || g.auth[nafathSendPath] != "Bearer tok" {
				t.Fatalf("request %v auth %q", got, g.auth[nafathSendPath])
			}
		})
	}
}

func TestNafathSendRequestFailures(t *testing.T) {
	g := newFakeGateway()
	c := newTestNafathClient(t, g, "mock")

	g.body[nafathSendPath] = `{"errorMessage":null,"checkStatus":null}`
	var noTrans *NafathRefusal
	if _, err := c.SendRequest(context.Background(), "tok", "1111111111"); !errors.As(err, &noTrans) || errors.Is(err, ErrNafathUnavailable) {
		t.Errorf("no transID: want Nafath's answer, got %v", err)
	}
	g.status[nafathSendPath] = http.StatusBadRequest
	g.body[nafathSendPath] = `{"errorMessage":null,"detailedCodeID":1011,"businessErrorMessage":"The ID number is not registered in Nafath."}`
	_, err := c.SendRequest(context.Background(), "tok", "1111111111")
	var refusal *NafathRefusal
	if !errors.As(err, &refusal) || refusal.Message != "The ID number is not registered in Nafath." {
		t.Errorf("business error: want refusal, got %v", err)
	}
	g.status[nafathSendPath] = http.StatusUnauthorized
	g.body[nafathSendPath] = `{"statusCode":401,"message":"Invalid Token."}`
	if _, err := c.SendRequest(context.Background(), "tok", "1111111111"); !errors.Is(err, ErrNafathTokenRejected) || !errors.As(err, &refusal) || refusal.Message != "Invalid Token." {
		t.Errorf("401: want token rejected with Nafath's \"Invalid Token.\", got %v", err)
	}
	g.status[nafathSendPath] = http.StatusBadGateway
	g.body[nafathSendPath] = ``
	if _, err := c.SendRequest(context.Background(), "tok", "1111111111"); !errors.As(err, &refusal) || refusal.Message != "502 Bad Gateway" {
		t.Errorf("502 with no body: want Nafath's status \"502 Bad Gateway\", got %v", err)
	}
	g.status[nafathSendPath] = http.StatusServiceUnavailable
	g.body[nafathSendPath] = `{"errorMessage":"Nafath is under maintenance"}`
	if _, err := c.SendRequest(context.Background(), "tok", "1111111111"); !errors.As(err, &refusal) || refusal.Message != "Nafath is under maintenance" {
		t.Errorf("503 with a message: want Nafath's message, got %v", err)
	}
}

func TestNafathCheckRequest(t *testing.T) {
	g := newFakeGateway()
	c := newTestNafathClient(t, g, "mock")
	req := NafathRequest{TransID: "aa-bb", Random: "53"}

	g.body[nafathCheckPath] = completedBody
	res, err := c.CheckRequest(context.Background(), "tok", "1111111111", req)
	if err != nil || res.Status != NafathStatusCompleted || res.Person == nil ||
		res.Person.IDNumber != "1111111111" || res.Person.FullNameEn != "AHMED ALALI" {
		t.Fatalf("COMPLETED = %+v %+v, %v", res, res.Person, err)
	}
	got := g.got[nafathCheckPath]
	if got["random"] != float64(53) || got["transID"] != "aa-bb" || got["idNumber"] != float64(1111111111) || got["appID"] != float64(17) {
		t.Fatalf("request body %v", got)
	}

	for raw, want := range map[string]string{"WAITING": NafathStatusWaiting, "REJECTED": NafathStatusRejected, "EXPIRED": NafathStatusExpired, "PENDING_VERIFICATION": NafathStatusWaiting} {
		g.body[nafathCheckPath] = `{"errorMessage":null,"checkStatus":"` + raw + `","userInfo":null}`
		res, err := c.CheckRequest(context.Background(), "tok", "1111111111", req)
		if err != nil || res.Status != want || res.Person != nil {
			t.Errorf("%s: got %+v, %v", raw, res, err)
		}
	}

	g.body[nafathCheckPath] = `{"errorMessage":null,"checkStatus":"COMPLETED","userInfo":null}`
	if res, err := c.CheckRequest(context.Background(), "tok", "1111111111", req); err != nil || res.Person != nil {
		t.Errorf("COMPLETED without userInfo must have no Person: %+v, %v", res, err)
	}

	// The UAT gateway answers HTTP 400 while waiting (2026-11-25 log): the
	// checkStatus decides, not the HTTP status.
	g.status[nafathCheckPath] = http.StatusBadRequest
	for raw, want := range map[string]string{"WAITING": NafathStatusWaiting, "REJECTED": NafathStatusRejected, "EXPIRED": NafathStatusExpired} {
		g.body[nafathCheckPath] = `{"accessToken":null,"businessErrorMessage":null,"checkStatus":"` + raw + `","detailedCode":null,"detailedCodeID":0,` +
			`"employeeList":null,"errorMessage":null,"exceptionMessage":null,"itemsCount":0,"person":null,"responseJSON":null,"userDelegations":null,"userInfo":null}`
		res, err := c.CheckRequest(context.Background(), "tok", "1111111111", req)
		if err != nil || res.Status != want {
			t.Errorf("400 with %s: got %+v, %v", raw, res, err)
		}
	}

	g.body[nafathCheckPath] = `{"errorMessage":null,"detailedCodeID":1013,"businessErrorMessage":"Request not found."}`
	// No checkStatus: Nafath's own message is returned as its refusal.
	var refusal *NafathRefusal
	if _, err := c.CheckRequest(context.Background(), "tok", "1111111111", req); !errors.As(err, &refusal) || refusal.Message != "Request not found." {
		t.Errorf("unknown transID: want Nafath's refusal \"Request not found.\", got %v", err)
	}

	// 400 with no checkStatus and no message: nothing from Nafath to show.
	g.body[nafathCheckPath] = `{"errorMessage":null,"checkStatus":null,"userInfo":null}`
	if _, err := c.CheckRequest(context.Background(), "tok", "1111111111", req); !errors.As(err, &refusal) || refusal.Message != "400 Bad Request" {
		t.Errorf("400 without status or message: want Nafath's status \"400 Bad Request\", got %v", err)
	}
}

func TestNafathRealModeRefusesSimulator(t *testing.T) {
	g := newFakeGateway()
	g.mock = true
	g.body[nafathTokenPath] = `{"accessToken":"tok"}`
	srv := httptest.NewTLSServer(g)
	t.Cleanup(srv.Close)
	c, err := NewNafathClient(config.NafathConfig{
		Mode: "real", BaseURL: srv.URL, Service: "svc", Key: "k", AppID: "13", CheckAppID: "13",
		TimeoutSeconds: 5, PollSeconds: 10, InsecureSkipVerify: true,
	}, "development")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetToken(context.Background(), "1111111111"); !errors.Is(err, ErrNafathUnavailable) {
		t.Fatalf("simulator response in real mode must be refused, got %v", err)
	}
}

func TestNewNafathClientValidation(t *testing.T) {
	base := config.NafathConfig{Mode: "real", BaseURL: "https://gw.example", Service: "s", Key: "k", AppID: "13", CheckAppID: "17",
		TimeoutSeconds: 15, PollSeconds: 10}
	cases := map[string]func(*config.NafathConfig, *string){
		"mock in production": func(c *config.NafathConfig, env *string) {
			c.Mode, c.BaseURL, *env = "mock", "http://localhost:8098", "production"
		},
		"http in real mode":         func(c *config.NafathConfig, _ *string) { c.BaseURL = "http://gw.example" },
		"missing key":               func(c *config.NafathConfig, _ *string) { c.Key = "" },
		"non-numeric appID":         func(c *config.NafathConfig, _ *string) { c.AppID = "abc" },
		"skip verify in production": func(c *config.NafathConfig, env *string) { c.InsecureSkipVerify, *env = true, "production" },
		"unknown mode":              func(c *config.NafathConfig, _ *string) { c.Mode = "test" },
	}
	for name, mutate := range cases {
		cfg, env := base, "development"
		mutate(&cfg, &env)
		if _, err := NewNafathClient(cfg, env); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := NewNafathClient(base, "production"); err != nil {
		t.Errorf("valid real config: %v", err)
	}
}

// Only a missing answer (no response at all) is ErrNafathUnavailable.
func TestNafathNoAnswerIsUnavailable(t *testing.T) {
	c, err := NewNafathClient(config.NafathConfig{
		Mode: "mock", BaseURL: "http://127.0.0.1:1", Service: "svc", Key: "k", AppID: "13", CheckAppID: "17",
		TimeoutSeconds: 1, PollSeconds: 10,
	}, "development")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SendRequest(context.Background(), "tok", "1111111111"); !errors.Is(err, ErrNafathUnavailable) {
		t.Errorf("connection refused: want ErrNafathUnavailable, got %v", err)
	}
}
