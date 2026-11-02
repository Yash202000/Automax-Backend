package handlers

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSendWithRange(t *testing.T) {
	data := []byte("0123456789")
	var fetched [][2]int64
	fetch := func(start, end int64) (io.ReadCloser, error) {
		fetched = append(fetched, [2]int64{start, end})
		return io.NopCloser(bytes.NewReader(data[start : end+1])), nil
	}
	app := fiber.New()
	app.Get("/f", func(c *fiber.Ctx) error {
		return sendWithRange(c, fetch, int64(len(data)), "audio/mpeg", "a.mp3", "inline")
	})

	cases := []struct {
		rng, body, cr string
		status        int
		want          [2]int64
	}{
		{"", "0123456789", "", 200, [2]int64{0, 9}},
		{"bytes=0-3", "0123", "bytes 0-3/10", 206, [2]int64{0, 3}},
		{"bytes=7-", "789", "bytes 7-9/10", 206, [2]int64{7, 9}},
		{"bytes=-4", "6789", "bytes 6-9/10", 206, [2]int64{6, 9}},
		{"bytes=5-100", "56789", "bytes 5-9/10", 206, [2]int64{5, 9}},
		{"bytes=10-", "", "bytes */10", 416, [2]int64{}},
		{"bytes=5-2", "", "bytes */10", 416, [2]int64{}},
		{"bytes=0-1,3-4", "", "bytes */10", 416, [2]int64{}},
		{"garbage", "", "bytes */10", 416, [2]int64{}},
	}
	for _, tc := range cases {
		fetched = nil
		req := httptest.NewRequest("GET", "/f", nil)
		if tc.rng != "" {
			req.Header.Set("Range", tc.rng)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != tc.status || resp.Header.Get("Content-Range") != tc.cr {
			t.Errorf("%q: got %d cr=%q", tc.rng, resp.StatusCode, resp.Header.Get("Content-Range"))
		}
		if tc.status != 416 && string(b) != tc.body {
			t.Errorf("%q: body %q, want %q", tc.rng, b, tc.body)
		}
		if tc.status == 416 {
			if len(fetched) != 0 {
				t.Errorf("%q: storage fetched on 416: %v", tc.rng, fetched)
			}
		} else if len(fetched) != 1 || fetched[0] != tc.want {
			t.Errorf("%q: fetched %v, want exactly %v", tc.rng, fetched, tc.want)
		}
		if resp.Header.Get("Accept-Ranges") != "bytes" {
			t.Errorf("%q: missing Accept-Ranges", tc.rng)
		}
	}
}

func TestSendWithRangeEmptyFile(t *testing.T) {
	fetch := func(start, end int64) (io.ReadCloser, error) {
		t.Errorf("storage must not be read for an empty file")
		return nil, io.EOF
	}
	app := fiber.New()
	app.Get("/f", func(c *fiber.Ctx) error {
		return sendWithRange(c, fetch, 0, "text/plain", "empty.txt", "attachment")
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/f", nil))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(b) != 0 || resp.ContentLength > 0 {
		t.Errorf("got %d len=%d content-length=%d, want 200 with empty body", resp.StatusCode, len(b), resp.ContentLength)
	}
}
