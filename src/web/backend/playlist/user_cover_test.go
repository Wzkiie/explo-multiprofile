package playlist

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"strings"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"explo/src/web/backend/app"
)

func upload(t *testing.T, p *Playlist, id string, data []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("image", "x.png")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/ui/playlists/"+id+"/cover", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	p.HandleUploadCover(rec, req)
	return rec
}

func TestUserCoverUploadNormalizesToJPEG(t *testing.T) {
	dir := t.TempDir()
	p := NewPlaylist(app.Config{WebDataDir: dir}, nil)

	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.NRGBA{R: 255, A: 128})
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)

	for _, id := range []string{"weekly-jams", "custom-abc123"} {
		if rec := upload(t, p, id, pngBuf.Bytes()); rec.Code != http.StatusNoContent {
			t.Fatalf("%s: upload status %d: %s", id, rec.Code, rec.Body.String())
		}
		raw, err := os.ReadFile(UserCoverPath(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("%s: stored cover is not a JPEG: %v", id, err)
		}
	}
	if rec := upload(t, p, "weekly-jams", []byte("not an image")); rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage must be rejected, got %d", rec.Code)
	}
	if rec := upload(t, p, "../etc", pngBuf.Bytes()); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id must be rejected, got %d", rec.Code)
	}
}

// a PNG that only claims to be 60000x60000 pixels must be rejected before it is decoded
func TestUserCoverRejectsHugeDimensions(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 60000)
	binary.BigEndian.PutUint32(ihdr[4:], 60000)
	ihdr[8], ihdr[9] = 8, 0 // 8 bit, grayscale
	chunk := append([]byte("IHDR"), ihdr...)
	binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))

	p := NewPlaylist(app.Config{WebDataDir: t.TempDir()}, nil)
	rec := upload(t, p, "weekly-jams", b.Bytes())
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too large") {
		t.Fatalf("expected 400 too large, got %d: %s", rec.Code, rec.Body.String())
	}
}
