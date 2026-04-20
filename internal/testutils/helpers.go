package testutils

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
)

type TestApp struct {
	*httptest.Server
	app *fiber.App
}

func NewTestApp() *TestApp {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error":   err.Error(),
			})
		},
	})
	return &TestApp{app: app}
}

func (t *TestApp) App() *fiber.App {
	return t.app
}

func (t *TestApp) Get(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	return req
}

func (t *TestApp) Post(path string, body interface{}) *http.Request {
	var b io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		b = bytes.NewReader(data)
	}
	req := httptest.NewRequest(http.MethodPost, path, b)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (t *TestApp) Put(path string, body interface{}) *http.Request {
	var b io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		b = bytes.NewReader(data)
	}
	req := httptest.NewRequest(http.MethodPut, path, b)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (t *TestApp) Delete(path string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	return req
}

func (t *TestApp) TestRequest(req *http.Request) (*http.Response, error) {
	return t.app.Test(req)
}

type MockContext struct {
	Ctx       *fiber.Ctx
	UserID    string
	Param     map[string]string
	Query     map[string]string
	Body      map[string]interface{}
	Locals    map[string]interface{}
	AuthToken string
}

func (m *MockContext) GetUserID() string {
	return m.UserID
}

type ResponseHelper struct {
	Success bool              `json:"success"`
	Message string            `json:"message,omitempty"`
	Data    interface{}       `json:"data,omitempty"`
	Error   string            `json:"error,omitempty"`
	Errors  []ValidationError `json:"errors,omitempty"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func ParseResponse(resp *http.Response) (ResponseHelper, error) {
	body, _ := io.ReadAll(resp.Body)
	var result ResponseHelper
	json.Unmarshal(body, &result)
	return result, nil
}

func ParseJSONBody(body io.Reader, target interface{}) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
