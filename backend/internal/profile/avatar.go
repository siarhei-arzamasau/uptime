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

func decodeAvatar(data []byte) (image.Image, string, error) {
	// Inspect dimensions before allocating decoded pixels; do not trust the upload MIME type.
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

// uploadAvatar validates and re-encodes image data before replacing the stored profile.
// @Summary Update the name and avatar
// @ID uploadAvatar
// @Tags profile
// @Description Requires exactly one name field and one avatar file. Name is trimmed and limited to 100 Unicode characters without controls; empty clears it. JPEG/PNG files are limited to 5 MiB and 2048 by 2048 pixels. Images are re-encoded without uploaded metadata.
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param name formData string true "Profile name; at most 100 Unicode characters after trimming; empty string clears it"
// @Param avatar formData file true "JPEG or PNG, at most 5 MiB and 2048 by 2048 pixels"
// @Success 200 {object} profileResponse "Updated profile"
// @Failure 400 {object} httpx.ErrorResponse "Invalid multipart fields, name, or image"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token or deleted user"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 413 {object} httpx.ErrorResponse "Avatar or multipart body too large"
// @Failure 500 {object} httpx.ErrorResponse "Unable to save avatar"
// @Router /profile/avatar [post]
func (c *Controller) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	// Allow multipart fields/boundaries in addition to the 5 MiB image budget.
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
	// Read one extra byte to detect oversize input instead of accepting a truncated image.
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
	// Re-encode decoded pixels so stored files omit uploaded metadata and trailing data.
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

// serveAvatar accepts only generated filenames to prevent traversal of the avatar directory.
// @Summary Get a public avatar
// @ID getAvatar
// @Tags profile
// @Description Serves a public JPEG or PNG by its generated lowercase UUID filename. Supports conditional and range requests through net/http ServeContent. Files are publicly cacheable for one year.
// @Produce png,jpeg
// @Param filename path string true "Lowercase UUID followed by .png or .jpg" example(550e8400-e29b-41d4-a716-446655440000.png)
// @Param Range header string false "Optional byte range" example(bytes=0-1023)
// @Param If-Modified-Since header string false "Optional HTTP timestamp for cache validation"
// @Param If-Range header string false "HTTP timestamp; serve the range only if the image is unchanged"
// @Success 200 {string} string "JPEG or PNG image bytes"
// @Failure 206 {string} string "Partial image bytes"
// @Failure 304 "Image not modified"
// @Failure 403 "JSON error envelope: Origin rejected"
// @Failure 404 "Plain text: avatar not found"
// @Failure 412 "Plain text: conditional request precondition failed"
// @Failure 416 "Plain text: unsatisfiable byte range"
// @Header 200,206 {string} Cache-Control "public, max-age=31536000, immutable"
// @Header 200,206 {string} Last-Modified "HTTP timestamp for conditional requests"
// @Router /avatars/{filename} [get]
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
