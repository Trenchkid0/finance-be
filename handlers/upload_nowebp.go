//go:build windows
// +build windows

package handlers

import (
	"image"
	"image/jpeg"
	"os"
)

// encodeImage encodes the image to JPEG format (Windows fallback)
func encodeImage(outputFile *os.File, img image.Image) (int64, error) {
	opts := &jpeg.Options{Quality: 90}
	err := jpeg.Encode(outputFile, img, opts)
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
	return ".jpg"
}

// getImageMimeType returns the MIME type for the encoded format
func getImageMimeType() string {
	return "image/jpeg"
}
