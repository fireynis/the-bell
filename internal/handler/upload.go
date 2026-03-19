package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxImageSize is the default maximum upload size (5 MB).
const maxImageSize int64 = 5 << 20

// Supported MIME types mapped to their canonical file extensions.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// errUnsupportedType is returned when the uploaded file is not a supported image format.
var errUnsupportedType = errors.New("unsupported image type: only JPEG, PNG, and WebP are allowed")

// errFileTooLarge is returned when the uploaded file exceeds the size limit.
var errFileTooLarge = errors.New("image too large: maximum size is 5 MB")

// parseImageUpload reads the "image" field from a multipart request, validates
// the MIME type by inspecting magic bytes, and enforces a size limit.
// It returns the raw file bytes and the detected file extension (e.g. ".jpg").
// If the "image" field is missing it returns (nil, "", nil).
func parseImageUpload(r *http.Request, maxSize int64) ([]byte, string, error) {
	file, header, err := r.FormFile("image")
	if err == http.ErrMissingFile {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading image field: %w", err)
	}
	defer file.Close()

	if header.Size > maxSize {
		return nil, "", errFileTooLarge
	}

	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading image data: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, "", errFileTooLarge
	}

	// Detect MIME type from magic bytes (first 512 bytes is sufficient).
	detected := http.DetectContentType(data)
	ext, ok := allowedImageTypes[detected]
	if !ok {
		// http.DetectContentType may not recognise WebP; check RIFF header.
		if isWebP(data) {
			ext = ".webp"
		} else {
			return nil, "", errUnsupportedType
		}
	}

	return data, ext, nil
}

// isWebP checks for the RIFF....WEBP magic bytes.
func isWebP(data []byte) bool {
	return len(data) >= 12 &&
		bytes.Equal(data[0:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP"))
}
