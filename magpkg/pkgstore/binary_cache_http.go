package pkgstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

const (
	binaryCacheInfoPath      = "/v1/info"
	binaryCachePackagesPath  = "/v1/packages/"
	binaryCacheFormat        = "magpkg-binary-cache"
	binaryCacheFormatVersion = 1
)

// BinaryCacheHandlerOptions controls serving package artifacts over HTTP.
type BinaryCacheHandlerOptions struct {
	// DisableTouch prevents GET requests for existing artifacts from refreshing
	// access and modification times. HEAD requests never touch artifacts.
	DisableTouch bool
}

// NewBinaryCacheHandler returns a versioned, read-only HTTP handler for the
// completed package artifacts in store.
func NewBinaryCacheHandler(
	store *Store,
	options BinaryCacheHandlerOptions,
) (http.Handler, error) {
	if err := validateStore(store); err != nil {
		return nil, err
	}
	return &binaryCacheHandler{
		store:   store,
		options: options,
	}, nil
}

type binaryCacheHandler struct {
	store   *Store
	options BinaryCacheHandlerOptions
}

func (handler *binaryCacheHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	switch request.URL.Path {
	case binaryCacheInfoPath:
		handler.serveInfo(writer, request)
	default:
		handler.servePackage(writer, request)
	}
}

func (handler *binaryCacheHandler) serveInfo(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if !allowBinaryCacheMethod(writer, request) {
		return
	}
	data, err := json.Marshal(struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}{
		Format:  binaryCacheFormat,
		Version: binaryCacheFormatVersion,
	})
	if err != nil {
		http.Error(writer, "encode cache information", http.StatusInternalServerError)
		return
	}
	data = append(data, '\n')
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Content-Length", strconv.Itoa(len(data)))
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	_, _ = writer.Write(data)
}

func (handler *binaryCacheHandler) servePackage(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if !allowBinaryCacheMethod(writer, request) {
		return
	}
	if len(request.URL.Path) <= len(binaryCachePackagesPath) ||
		request.URL.Path[:len(binaryCachePackagesPath)] !=
			binaryCachePackagesPath {
		http.NotFound(writer, request)
		return
	}
	filename := request.URL.Path[len(binaryCachePackagesPath):]
	if filepath.Base(filename) != filename {
		http.NotFound(writer, request)
		return
	}
	if _, ok := packageArtifactEntryID(filename); !ok {
		http.NotFound(writer, request)
		return
	}

	artifactPath := filepath.Join(
		handler.store.path,
		packageDirectoryName,
		filename,
	)
	file, err := openRegularFileNoFollow(artifactPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) ||
			errors.Is(err, unix.ENOTDIR) ||
			errors.Is(err, unix.ELOOP) {
			http.NotFound(writer, request)
			return
		}
		http.Error(writer, "open package artifact", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if request.Method == http.MethodGet && !handler.options.DisableTouch {
		if err := touchOpenFile(file); err != nil {
			http.Error(writer, "touch package artifact", http.StatusInternalServerError)
			return
		}
	}
	writer.Header().Set("Content-Type", "application/zstd")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set(
		"Cache-Control",
		"public, max-age=31536000, immutable",
	)
	http.ServeContent(writer, request, filename, time.Time{}, file)
}

func allowBinaryCacheMethod(
	writer http.ResponseWriter,
	request *http.Request,
) bool {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
}

func openRegularFileNoFollow(path string) (*os.File, error) {
	descriptor, err := unix.Open(
		path,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(descriptor), path)
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, errors.Join(
			fmt.Errorf("package artifact %q is not a regular file", path),
			file.Close(),
		)
	}
	return file, nil
}

func touchOpenFile(file *os.File) error {
	now := unix.NsecToTimeval(time.Now().UnixNano())
	return unix.Futimes(
		int(file.Fd()),
		[]unix.Timeval{now, now},
	)
}
