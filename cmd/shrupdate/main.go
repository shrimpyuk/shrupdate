package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var version, genDir string

type current struct {
	Version string `json:"version"`
	Sha256  string `json:"sha256"`
}

func generateSha256(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func createGzipFile(path string, targetPath string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer w.Close()

	gz := gzip.NewWriter(w)
	defer gz.Close()

	_, err = io.Copy(gz, f)
	return err
}

func createUpdate(wg *sync.WaitGroup, path string, platform string, updates chan<- string, errors chan<- error) {
	defer wg.Done()

	checksum, err := generateSha256(path)
	if err != nil {
		errors <- err
		return
	}

	c := current{Version: version, Sha256: fmt.Sprintf("%x", checksum)}

	jsonPath := filepath.Join(genDir, platform+".json")
	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		errors <- err
		return
	}
	defer jsonFile.Close()

	enc := json.NewEncoder(jsonFile)
	enc.SetIndent("", "    ")
	if err := enc.Encode(c); err != nil {
		errors <- err
		return
	}

	os.MkdirAll(filepath.Join(genDir, version), 0755)

	gzipPath := filepath.Join(genDir, version, platform+".gz")
	if err := createGzipFile(path, gzipPath); err != nil {
		errors <- err
		return
	}

	// Diff generation goes here...

	updates <- platform
}

func main() {
	outputDirFlag := flag.String("o", "public", "Output directory for writing updates")
	platformFlag := flag.String("platform", runtime.GOOS+"-"+runtime.GOARCH,
		"Target platform in the form OS-ARCH. Defaults to running os/arch or the combination of the environment variables GOOS and GOARCH if both are set.")

	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Println("Usage: <application> <version>")
		os.Exit(1)
	}

	platform := *platformFlag
	appPath := flag.Arg(0)
	version = flag.Arg(1)
	genDir = *outputDirFlag

	if err := os.MkdirAll(genDir, 0755); err != nil {
		fmt.Println("Error creating directory:", err)
		os.Exit(1)
	}

	files, err := os.ReadDir(appPath)
	if err != nil {
		fmt.Println("Error reading directory:", err)
		os.Exit(1)
	}

	var wg sync.WaitGroup
	updates := make(chan string)
	errors := make(chan error)

	for _, file := range files {
		wg.Add(1)
		go createUpdate(&wg, filepath.Join(appPath, file.Name()), platform, updates, errors)
	}

	go func() {
		wg.Wait()
		close(updates)
		close(errors)
	}()

	for {
		select {
		case platform, ok := <-updates:
			if !ok {
				updates = nil
			} else {
				fmt.Println("Update created for platform:", platform)
			}
		case err, ok := <-errors:
			if !ok {
				errors = nil
			} else {
				fmt.Println("Error:", err)
			}
		}
		if updates == nil && errors == nil {
			break
		}
	}
}
