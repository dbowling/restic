package backend

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"testing"

	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/mock"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/test"
)

func TestBackendRetrySeeker(t *testing.T) {
	be := &mock.Backend{
		SaveFn: func(ctx context.Context, h restic.Handle, rd io.Reader) error {
			return nil
		},
	}

	retryBackend := RetryBackend{
		Backend: be,
	}

	data := test.Random(24, 23*14123)

	type wrapReader struct {
		io.Reader
	}

	var rd io.Reader
	rd = wrapReader{bytes.NewReader(data)}

	err := retryBackend.Save(context.TODO(), restic.Handle{}, rd)
	if err == nil {
		t.Fatal("did not get expected error for retry backend with non-seeker reader")
	}

	rd = bytes.NewReader(data)
	_, err = io.CopyN(ioutil.Discard, rd, 5)
	if err != nil {
		t.Fatal(err)
	}

	err = retryBackend.Save(context.TODO(), restic.Handle{}, rd)
	if err == nil {
		t.Fatal("did not get expected error for partial reader")
	}
}

func TestBackendSaveRetry(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	errcount := 0
	be := &mock.Backend{
		SaveFn: func(ctx context.Context, h restic.Handle, rd io.Reader) error {
			if errcount == 0 {
				errcount++
				_, err := io.CopyN(ioutil.Discard, rd, 120)
				if err != nil {
					return err
				}

				return errors.New("injected error")
			}

			_, err := io.Copy(buf, rd)
			return err
		},
	}

	retryBackend := RetryBackend{
		Backend: be,
	}

	data := test.Random(23, 5*1024*1024+11241)
	err := retryBackend.Save(context.TODO(), restic.Handle{}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != buf.Len() {
		t.Errorf("wrong number of bytes written: want %d, got %d", len(data), buf.Len())
	}

	if !bytes.Equal(data, buf.Bytes()) {
		t.Errorf("wrong data written to backend")
	}
}

func TestBackendListRetry(t *testing.T) {
	const (
		ID1 = "id1"
		ID2 = "id2"
	)

	retry := 0
	be := &mock.Backend{
		ListFn: func(ctx context.Context, t restic.FileType, fn func(restic.FileInfo) error) error {
			// fail during first retry, succeed during second
			retry++
			if retry == 1 {
				fn(restic.FileInfo{Name: ID1})
				return errors.New("test list error")
			}
			fn(restic.FileInfo{Name: ID1})
			fn(restic.FileInfo{Name: ID2})
			return nil
		},
	}

	retryBackend := RetryBackend{
		Backend: be,
	}

	var listed []string
	err := retryBackend.List(context.TODO(), restic.DataFile, func(fi restic.FileInfo) error {
		listed = append(listed, fi.Name)
		return nil
	})
	test.OK(t, err)                            // assert overall success
	test.Equals(t, 2, retry)                   // assert retried once
	test.Equals(t, []string{ID1, ID2}, listed) // assert no duplicate files
}
