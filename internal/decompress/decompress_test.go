package decompress

import (
	"bytes"
	"compress/gzip"
	"errors"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func compressGzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func compressZstd(t *testing.T, data []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd new writer: %v", err)
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil)
}

func TestDecompress(t *testing.T) {
	sample := []byte(`{"ref":"refs/heads/main","repository":{"full_name":"UnitVectorY-Labs/demo"}}`)
	gzipSample := compressGzip(t, sample)
	zstdSample := compressZstd(t, sample)

	tests := []struct {
		name            string
		data            []byte
		attrs           map[string]string
		compressionAttr string
		maxBytes        int64
		want            []byte
		wantErr         error
	}{
		{
			name:            "uncompressed absent attribute",
			data:            sample,
			attrs:           map[string]string{},
			compressionAttr: "compression",
			maxBytes:        1024,
			want:            sample,
		},
		{
			name:            "uncompressed explicit none",
			data:            sample,
			attrs:           map[string]string{"compression": "none"},
			compressionAttr: "compression",
			maxBytes:        1024,
			want:            sample,
		},
		{
			name:            "gzip compressed",
			data:            gzipSample,
			attrs:           map[string]string{"compression": "gzip"},
			compressionAttr: "compression",
			maxBytes:        1024,
			want:            sample,
		},
		{
			name:            "zstd compressed",
			data:            zstdSample,
			attrs:           map[string]string{"compression": "zstd"},
			compressionAttr: "compression",
			maxBytes:        1024,
			want:            sample,
		},
		{
			name:            "custom compression attribute name",
			data:            zstdSample,
			attrs:           map[string]string{"encoding": "ZSTD"},
			compressionAttr: "encoding",
			maxBytes:        1024,
			want:            sample,
		},
		{
			name:            "decompression disabled when attribute empty",
			data:            zstdSample,
			attrs:           map[string]string{"compression": "zstd"},
			compressionAttr: "",
			maxBytes:        1024,
			want:            zstdSample,
		},
		{
			name:            "unknown compression algorithm",
			data:            sample,
			attrs:           map[string]string{"compression": "brotli"},
			compressionAttr: "compression",
			maxBytes:        1024,
			wantErr:         ErrUnsupportedCompression,
		},
		{
			name:            "corrupt gzip data",
			data:            []byte("not valid gzip"),
			attrs:           map[string]string{"compression": "gzip"},
			compressionAttr: "compression",
			maxBytes:        1024,
			wantErr:         ErrDecompressFailed,
		},
		{
			name:            "corrupt zstd data",
			data:            []byte("not valid zstd"),
			attrs:           map[string]string{"compression": "zstd"},
			compressionAttr: "compression",
			maxBytes:        1024,
			wantErr:         ErrDecompressFailed,
		},
		{
			name:            "uncompressed exceeds max payload bytes",
			data:            bytes.Repeat([]byte("a"), 100),
			attrs:           nil,
			compressionAttr: "compression",
			maxBytes:        50,
			wantErr:         ErrPayloadTooLarge,
		},
		{
			name:            "gzip decompressed exceeds max payload bytes",
			data:            compressGzip(t, bytes.Repeat([]byte("a"), 4096)),
			attrs:           map[string]string{"compression": "gzip"},
			compressionAttr: "compression",
			maxBytes:        512,
			wantErr:         ErrPayloadTooLarge,
		},
		{
			name:            "zstd decompressed exceeds max payload bytes",
			data:            compressZstd(t, bytes.Repeat([]byte("a"), 4096)),
			attrs:           map[string]string{"compression": "zstd"},
			compressionAttr: "compression",
			maxBytes:        512,
			wantErr:         ErrPayloadTooLarge,
		},
		{
			name:            "exact max payload boundary passes",
			data:            compressZstd(t, bytes.Repeat([]byte("x"), 64)),
			attrs:           map[string]string{"compression": "zstd"},
			compressionAttr: "compression",
			maxBytes:        64,
			want:            bytes.Repeat([]byte("x"), 64),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decompress(tc.data, tc.attrs, tc.compressionAttr, tc.maxBytes)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Decompress() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decompress() unexpected error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Decompress() = %q, want %q", got, tc.want)
			}
		})
	}
}
