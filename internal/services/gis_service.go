package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	gisTokenURL    = "https://webgis.eamana.gov.sa/arcgisnew/tokens/generateToken"
	gisIdentifyURL = "https://webgis.eamana.gov.sa/arcgisnew/rest/services/940BaseMap/MapServer/identify"
)

type GISService interface {
	Identify(ctx context.Context, x, y float64) (json.RawMessage, error)
}

type gisService struct {
	mu         sync.Mutex
	token      string
	tokenExpAt time.Time
	username   string
	password   string
	client     *http.Client
}

type gisTokenResponse struct {
	Token   string `json:"token"`
	Expires int64  `json:"expires"` // milliseconds since epoch
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewGISService() GISService {
	return &gisService{
		username: os.Getenv("GIS_USERNAME"),
		password: os.Getenv("GIS_PASSWORD"),
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *gisService) fetchToken(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("username", s.username)
	form.Set("password", s.password)
	form.Set("client", "requestip")
	form.Set("referer", "")
	form.Set("ip", "")
	form.Set("expiration", "60")
	form.Set("encrypted", "false")
	form.Set("f", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gisTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tr gisTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("token parse failed: %w", err)
	}
	if tr.Error != nil {
		return "", fmt.Errorf("GIS token error %d: %s", tr.Error.Code, tr.Error.Message)
	}

	s.token = tr.Token
	// subtract 2-minute buffer so we refresh before expiry
	s.tokenExpAt = time.UnixMilli(tr.Expires).Add(-2 * time.Minute)
	return s.token, nil
}

func (s *gisService) getToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExpAt) {
		return s.token, nil
	}
	return s.fetchToken(ctx)
}

var gisMockResponse = json.RawMessage(`{"results":[{"layerId":0,"layerName":"Municipality_Boundary","displayFieldName":"MUNICIPALITY_NAME","value":"??????","attributes":{"OBJECTID":"8044","MUNICIPALITY_NAME":"??????","SHAPE":"Polygon"}},{"layerId":2,"layerName":"District_Boundary","displayFieldName":"REMARKS","value":"??????? ???????","attributes":{"OBJECTID":"6393","DISTRICT_NAME":"?? ??????? ???????","SHAPE":"Polygon"}},{"layerId":3,"layerName":"Plan_Data","displayFieldName":"PLAN_NO","value":"? ? 1015","attributes":{"OBJECTID":"97209","PLAN_NO":"? ? 1015","SHAPE":"Polygon"}},{"layerId":5,"layerName":"Street_Naming","displayFieldName":"STREET_FULLNAME","value":"2?","attributes":{"OBJECTID":"12378","STREET_FULLNAME":"2?","SHAPE.LEN":"702.749001"}},{"layerId":5,"layerName":"Street_Naming","displayFieldName":"STREET_FULLNAME","value":"?????? ????????","attributes":{"OBJECTID":"12525","STREET_FULLNAME":"?????? ????????","SHAPE.LEN":"349.969662"}}]}`)

func (s *gisService) Identify(ctx context.Context, x, y float64) (json.RawMessage, error) {
	if strings.TrimSpace(s.username) == "" || strings.TrimSpace(s.password) == "" {
		return gisMockResponse, nil
	}

	token, err := s.getToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("GIS auth failed: %w", err)
	}

	const delta = 0.15
	mapExtent := fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", x-delta, y-delta, x+delta, y+delta)

	form := url.Values{}
	form.Set("geometry", fmt.Sprintf("{x:%f,y:%f}", x, y))
	form.Set("geometryType", "esriGeometryPoint")
	form.Set("sr", "4326")
	form.Set("layers", "all")
	form.Set("layerDefs", "")
	form.Set("time", "")
	form.Set("layerTimeOptions", "")
	form.Set("tolerance", "2")
	form.Set("mapExtent", mapExtent)
	form.Set("imageDisplay", "1600,1200,96")
	form.Set("returnGeometry", "false")
	form.Set("maxAllowableOffset", "")
	form.Set("geometryPrecision", "")
	form.Set("dynamicLayers", "")
	form.Set("returnZ", "false")
	form.Set("returnM", "false")
	form.Set("gdbVersion", "")
	form.Set("historicMoment", "")
	form.Set("returnUnformattedValues", "false")
	form.Set("returnFieldName", "true")
	form.Set("datumTransformations", "")
	form.Set("layerParameterValues", "")
	form.Set("mapRangeValues", "")
	form.Set("layerRangeValues", "")
	form.Set("f", "pjson")
	form.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gisIdentifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GIS identify failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(body), nil
}
