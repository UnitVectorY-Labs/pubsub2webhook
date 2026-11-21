// Package decompress handles bounded decompression of Pub/Sub message payloads.
package decompress

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const zstdMaxMemory = 64 << 20 // 64 MiB

var (
	// ErrPayloadTooLarge is returned when a payload exceeds MaxPayloadBytes.
	ErrPayloadTooLarge = errors.New("payload exceeds maximum allowed size")
	// ErrUnsupportedCompression is returned for unknown compression attribute values.
	ErrUnsupportedCompression = errors.New("unsupported payload compression")
	// ErrDecompressFailed is returned when compressed bytes cannot be decoded.
	ErrDecompressFailed = errors.New("failed to decompress payload")
)

// Decompress optionally decompresses data according to attrs[compressionAttr],
// enforcing maxBytes on the resulting payload. If compressionAttr is empty,
// decompression is disabled and only maxBytes is checked. Returned errors never
// include raw payload contents.
func Decompress(data []byte, attrs map[string]string, compressionAttr string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("max payload bytes must be positive")
	}

	algo := ""
	if compressionAttr != "" && attrs != nil {
		algo = strings.ToLower(strings.TrimSpace(attrs[compressionAttr]))
	}

	switch algo {
	case "", "none":
		if int64(len(data)) > maxBytes {
			return nil, ErrPayloadTooLarge
		}
		return data, nil

	case "gzip":
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid gzip stream", ErrDecompressFailed)
		}
		defer r.Close()
		return readBounded(r, maxBytes)

	case "zstd":
		r, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderMaxMemory(zstdMaxMemory))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid zstd stream", ErrDecompressFailed)
		}
		defer r.Close()
		return readBounded(r, maxBytes)

	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCompression, algo)
	}
}

func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	out, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: stream read error", ErrDecompressFailed)
	}
	if int64(len(out)) > maxBytes {
		return nil, ErrPayloadTooLarge
	}
	return out, nil
}
