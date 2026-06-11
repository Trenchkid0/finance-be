//go:build !windows
// +build !windows

package handlers

import (
	"image"
	"os"

	"github.com/bep/gowebp/libwebp"
	"github.com/bep/gowebp/libwebp/webpoptions"
)

// encodeImage encodes the image to WebP format (non-Windows platforms)
func encodeImage(outputFile *os.File, img image.Image) (int64, error) {
	err := libwebp.Encode(outputFile, img, webpoptions.EncodingOptions{
		Quality: WebPQuality,
	})
	if err != nil {
		return 0, err
	}

	fileInfo, err := outputFile.Stat()
	if err != nil {
		return 0, err
	}

	return fileInfo.Size(), nil
}

// getImageExtension returns the file extension for the encoded format
func getImageExtension() string {
	return ".webp"
}

// getImageMimeType returns the MIME type for the encoded format
func getImageMimeType() string {
	return "image/webp"
}
