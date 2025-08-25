/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package imagehandler

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
)

type InvalidBaseImageError struct {
	cause error
}

func (ie InvalidBaseImageError) Error() string {
	return "Base Image not available"
}

func (ie InvalidBaseImageError) Unwrap() error {
	return ie.cause
}

// imageFileSystem is an http.FileSystem that creates a virtual filesystem of
// host images.
type imageFileSystem struct {
	isoFile       *baseIso
	initramfsFile *baseInitramfs
	baseURL       *url.URL
	keys          map[string]string
	images        map[string]*imageFile
	mu            *sync.Mutex
	log           logr.Logger
}

var _ ImageHandler = &imageFileSystem{}
var _ http.FileSystem = &imageFileSystem{}

type ImageHandler interface {
	FileSystem() http.FileSystem
	ServeImage(key string, ignitionContent []byte, initramfs, static bool) (string, error)
	RemoveImage(key string)
}

func NewImageHandler(logger logr.Logger, isoFile, initramfsFile string, baseURL *url.URL) ImageHandler {
	return &imageFileSystem{
		log:           logger,
		isoFile:       newBaseIso(isoFile),
		initramfsFile: newBaseInitramfs(initramfsFile),
		baseURL:       baseURL,
		keys:          map[string]string{},
		images:        map[string]*imageFile{},
		mu:            &sync.Mutex{},
	}
}

func (f *imageFileSystem) FileSystem() http.FileSystem {
	return f
}

func (f *imageFileSystem) getBaseImage(initramfs bool) baseFile {
	if initramfs {
		return f.initramfsFile
	} else {
		return f.isoFile
	}
}

func (f *imageFileSystem) getNameForKey(key string) (name string, err error) {
	if img, exists := f.images[key]; exists {
		return img.name, nil
	}
	rand, err := uuid.NewRandom()
	if err == nil {
		name = rand.String()
	}
	return
}

func (f *imageFileSystem) ServeImage(key string, ignitionContent []byte, initramfs, static bool) (string, error) {
	size, err := f.getBaseImage(initramfs).Size()
	if err != nil {
		return "", InvalidBaseImageError{cause: err}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	name := key
	if !static {
		name, err = f.getNameForKey(key)
		if err != nil {
			return "", err
		}
	}
	p, err := url.Parse(fmt.Sprintf("/%s", name))
	if err != nil {
		return "", err
	}

	if _, exists := f.images[key]; !exists {
		f.keys[name] = key
		f.images[key] = &imageFile{
			name:            name,
			size:            size,
			ignitionContent: ignitionContent,
			initramfs:       initramfs,
		}
	}

	return f.baseURL.ResolveReference(p).String(), nil
}

func (f *imageFileSystem) imageFileByName(name string) *imageFile {
	f.mu.Lock()
	defer f.mu.Unlock()

	if key, exists := f.keys[name]; exists {
		return f.images[key]
	}
	return nil
}

func (f *imageFileSystem) RemoveImage(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if img, exists := f.images[key]; exists {
		delete(f.keys, img.name)
		delete(f.images, key)
	}
}
