package kerbetor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const DownloadedBytesRefreshRate = 200 * time.Millisecond

func GetRemoteFileSize(sourceUrl string, httpClient *http.Client) (uint64, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	resp, err := httpClient.Head(sourceUrl)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		if size, ok := parseContentLength(resp.Header.Get("Content-Length")); ok {
			return size, nil
		}
	}

	size, err := getRemoteFileSizeFromRange(sourceUrl, httpClient)
	if err != nil {
		return 0, fmt.Errorf("remote file size unknown: %s", err)
	}
	return size, nil
}

func DownloadFileChunk(sourceUrl string, destinationPath string, startOffset int64, endOffset int64, httpClient *http.Client) (int64, error) {
	// init http client
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	// get remote file size to check start-end offsets
	contentLength, err := GetRemoteFileSize(sourceUrl, httpClient)
	if err != nil {
		return 0, fmt.Errorf("error getting remote file size: %s", err)
	}
	if startOffset < 0 || endOffset < 0 || startOffset > endOffset || endOffset > int64(contentLength) {
		return 0, fmt.Errorf("invalid range: %d-%d", startOffset, endOffset)
	}

	// download file chunk
	req, _ := http.NewRequest("GET", sourceUrl, nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startOffset, endOffset))
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error downloading file chunk %d-%d: %s", startOffset, endOffset, err)
	}
	defer resp.Body.Close()

	// copy downloaded chunk to destination file
	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		return 0, fmt.Errorf("error creating destination file: %s", err)
	}
	defer destinationFile.Close()
	n, err := io.Copy(destinationFile, resp.Body)
	if err != nil {
		return 0, fmt.Errorf("error writing destination file: %s", err)
	}

	// return number of bytes downloaded
	return n, nil
}

func parseContentLength(header string) (uint64, bool) {
	if header == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(header, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return uint64(value), true
}

func getRemoteFileSizeFromRange(sourceUrl string, httpClient *http.Client) (uint64, error) {
	req, _ := http.NewRequest("GET", sourceUrl, nil)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("range probe failed: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		if size, ok := parseContentLength(resp.Header.Get("Content-Length")); ok && resp.StatusCode == http.StatusOK {
			return size, fmt.Errorf("server did not honor range request")
		}
		return 0, fmt.Errorf("server did not honor range request (status %d)", resp.StatusCode)
	}

	size, err := parseContentRangeTotal(resp.Header.Get("Content-Range"))
	if err != nil {
		return 0, err
	}
	if size == 0 {
		return 0, fmt.Errorf("remote size is zero")
	}
	return size, nil
}

func parseContentRangeTotal(header string) (uint64, error) {
	if header == "" {
		return 0, fmt.Errorf("missing Content-Range header")
	}
	parts := strings.SplitN(header, "/", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid Content-Range header: %s", header)
	}
	total := strings.TrimSpace(parts[1])
	if total == "*" {
		return 0, fmt.Errorf("unknown total size in Content-Range header")
	}
	value, err := strconv.ParseUint(total, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Content-Range total: %s", total)
	}
	return value, nil
}

func DownloadFileChunkAsync(sourceUrl string, destinationPath string, startOffset uint64, endOffset uint64, httpClient *http.Client) (chan uint64, chan error) {
	bytesDownloadedCh := make(chan uint64)
	errorCh := make(chan error, 1)

	go func() {
		defer close(bytesDownloadedCh)
		defer close(errorCh)

		if httpClient == nil {
			httpClient = &http.Client{}
		}

		if endOffset < startOffset {
			errorCh <- fmt.Errorf("invalid range: %d-%d", startOffset, endOffset)
			return
		}

		expectedSize := endOffset - startOffset + 1
		var existingSize uint64
		if exists, err := FileExists(destinationPath); err != nil {
			errorCh <- fmt.Errorf("cannot check destination file: %s", err)
			return
		} else if exists {
			size, err := GetFileSize(destinationPath)
			if err != nil {
				errorCh <- fmt.Errorf("cannot get destination file size: %s", err)
				return
			}
			existingSize = size
		}

		if existingSize > expectedSize {
			errorCh <- fmt.Errorf("existing chunk is larger than expected: %d > %d", existingSize, expectedSize)
			return
		}
		if existingSize == expectedSize {
			bytesDownloadedCh <- expectedSize
			return
		}

		rangeStart := startOffset + existingSize
		req, _ := http.NewRequest("GET", sourceUrl, nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, endOffset))
		req.Header.Set("User-Agent", "kerbetor")

		resp, err := httpClient.Do(req)
		if err != nil {
			errorCh <- fmt.Errorf("error downloading file chunk %d-%d: %s", rangeStart, endOffset, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusPartialContent {
			errorCh <- fmt.Errorf("server did not honor range request (status %d)", resp.StatusCode)
			return
		}

		var destinationFile *os.File
		if existingSize > 0 {
			destinationFile, err = os.OpenFile(destinationPath, os.O_WRONLY|os.O_APPEND, 0644)
		} else {
			destinationFile, err = os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		}
		if err != nil {
			errorCh <- fmt.Errorf("error creating destination file: %s", err)
			return
		}
		defer destinationFile.Close()

		bytesDownloadedCh <- existingSize

		buf := make([]byte, 32*1024)
		downloaded := existingSize
		lastUpdate := time.Now()
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				remaining := expectedSize - downloaded
				if uint64(n) > remaining {
					n = int(remaining)
				}
				if _, writeErr := destinationFile.Write(buf[:n]); writeErr != nil {
					errorCh <- fmt.Errorf("error writing destination file: %s", writeErr)
					return
				}
				downloaded += uint64(n)
				if time.Since(lastUpdate) >= DownloadedBytesRefreshRate {
					bytesDownloadedCh <- downloaded
					lastUpdate = time.Now()
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				errorCh <- fmt.Errorf("error downloading file chunk %d-%d: %s", rangeStart, endOffset, readErr)
				return
			}
			if downloaded == expectedSize {
				break
			}
		}

		if downloaded != expectedSize {
			errorCh <- fmt.Errorf("incomplete chunk download: %d/%d", downloaded, expectedSize)
			return
		}

		bytesDownloadedCh <- downloaded

	}()

	return bytesDownloadedCh, errorCh
}
