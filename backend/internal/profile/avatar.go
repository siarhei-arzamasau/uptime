package profile

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
)

const maxAvatarBytes = 5 << 20

var avatarFilename = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(png|jpg)$`)
var errInvalidImage = errors.New("invalid image")

// Decode the actual image and encode a fresh file, stripping metadata and trailing data.
func decodeAvatar(data []byte) (image.Image, string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return nil, "", errInvalidImage
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", errInvalidImage
	}
	ext := ".png"
	if format == "jpeg" {
		ext = ".jpg"
	}
	return img, ext, nil
}
func (c *Controller) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+(64<<10))
	err := r.ParseMultipartForm(maxAvatarBytes + (64 << 10))
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, 413, "avatar_too_large", "Choose an image under 5 MB")
			return
		}
		httpx.WriteError(w, 400, "invalid_avatar", "Choose a JPEG or PNG image")
		return
	}
	form := r.MultipartForm
	if form == nil || len(form.File) != 1 || len(form.File["avatar"]) != 1 || len(form.Value) != 1 || len(form.Value["name"]) != 1 {
		httpx.WriteError(w, 400, "invalid_request", "Provide only name and one avatar file")
		return
	}
	name := strings.TrimSpace(form.Value["name"][0])
	if !validName(name) {
		httpx.WriteError(w, 400, "invalid_name", "Name must be at most 100 characters without control characters")
		return
	}
	part := form.File["avatar"][0]
	if part.Size > maxAvatarBytes {
		httpx.WriteError(w, 413, "avatar_too_large", "Choose an image under 5 MB")
		return
	}
	file, err := part.Open()
	if err != nil {
		avatarError(w)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		avatarError(w)
		return
	}
	if len(data) > maxAvatarBytes {
		httpx.WriteError(w, 413, "avatar_too_large", "Choose an image under 5 MB")
		return
	}
	img, ext, err := decodeAvatar(data)
	if err != nil {
		httpx.WriteError(w, 400, "invalid_avatar", "Choose a valid JPEG or PNG up to 2048 × 2048 pixels")
		return
	}
	if err = os.MkdirAll(c.avatarDir, 0700); err != nil {
		avatarError(w)
		return
	}
	temp, err := os.CreateTemp(c.avatarDir, ".upload-*")
	if err != nil {
		avatarError(w)
		return
	}
	defer os.Remove(temp.Name())
	if ext == ".png" {
		err = png.Encode(temp, img)
	} else {
		err = jpeg.Encode(temp, img, &jpeg.Options{Quality: 90})
	}
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		avatarError(w)
		return
	}
	filename := uuid.NewString() + ext
	target := filepath.Join(c.avatarDir, filename)
	if err = os.Rename(temp.Name(), target); err != nil {
		avatarError(w)
		return
	}
	u, old, err := c.store.UpdateUserAvatar(r.Context(), id, name, filename)
	if err != nil {
		os.Remove(target)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.WriteError(w, 401, "unauthorized", "User no longer exists")
			return
		}
		avatarError(w)
		return
	}
	if avatarFilename.MatchString(old) {
		if err := os.Remove(filepath.Join(c.avatarDir, old)); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("previous avatar cleanup failed")
		}
	}
	respondProfile(w, u)
}
func avatarError(w http.ResponseWriter) {
	slog.Error("avatar save failed")
	httpx.WriteError(w, 500, "internal_error", "Unable to save avatar. Please try again")
}
func (c *Controller) serveAvatar(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !avatarFilename.MatchString(filename) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(c.avatarDir, filename))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	mime := "image/png"
	if strings.HasSuffix(filename, ".jpg") {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}
