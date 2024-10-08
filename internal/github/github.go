package github

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

var firstDirNameRegex = regexp.MustCompile("^[^/]*/")

func GetSha(ctx context.Context, owner string, repo string, revision *string, token *string) (string, error) {
	var url = "https://api.github.com/repos/" + owner + "/" + repo + "/commits/"
	if revision != nil {
		url += *revision
	} else {
		url += "HEAD"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Accept", "application/vnd.github.sha")
	if token != nil {
		req.Header.Add("Authorization", "Bearer "+*token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Non 200 response code, response=%#v", res)
	}

	sha, err := io.ReadAll(res.Body)
	return string(sha), err
}

func DownloadRepository(ctx context.Context, owner string, repo string, revision *string, token *string, destinationDir string) error {
	var url = "https://api.github.com/repos/" + owner + "/" + repo + "/tarball"
	if revision != nil {
		url += "/" + *revision
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if token != nil {
		req.Header.Add("Authorization", "Bearer "+*token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Non 200 response code, response=%#v", res)
	}

	return untar(destinationDir, res.Body)
}

// source: https://medium.com/@skdomino/taring-untaring-files-in-go-6b07cf56bc07
func untar(destinationDir string, tarSource io.Reader) error {
	gzr, err := gzip.NewReader(tarSource)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil

		case err != nil:
			return err

		case header == nil:
			continue
		}

		target := filepath.Join(destinationDir, firstDirNameRegex.ReplaceAllString(header.Name, ""))

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			f.Close()
		}
	}
}
