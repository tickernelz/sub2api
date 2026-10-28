package perch

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
)

type decodedBody struct {
	reader io.Reader
	close  func() error
}

func (d decodedBody) Read(p []byte) (int, error) { return d.reader.Read(p) }

func (d decodedBody) Close() error { return d.close() }

func decodeResponseBody(resp *http.Response) (io.ReadCloser, error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("content-encoding")))
	switch encoding {
	case "", "identity":
		return resp.Body, nil
	case "gzip", "x-gzip":
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return decodedBody{reader: reader, close: func() error {
			readerErr := reader.Close()
			bodyErr := resp.Body.Close()
			if readerErr != nil {
				return readerErr
			}
			return bodyErr
		}}, nil
	case "deflate":
		reader, err := zlib.NewReader(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return decodedBody{reader: reader, close: func() error {
			readerErr := reader.Close()
			bodyErr := resp.Body.Close()
			if readerErr != nil {
				return readerErr
			}
			return bodyErr
		}}, nil
	case "br":
		return decodedBody{reader: brotli.NewReader(resp.Body), close: resp.Body.Close}, nil
	}
	return resp.Body, nil
}
