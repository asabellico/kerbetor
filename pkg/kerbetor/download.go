package kerbetor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/corpix/uarand"
)

const DownloadedBytesRefreshRate = 200 * time.Millisecond

func GetRemoteFileSize(sourceUrl string, httpClient *http.Client) (uint64, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	resp, err := httpClient.Head(sourceUrl)
	if err != nil {
		return 0, fmt.Errorf("error getting file size: %s", err)
	}
	defer resp.Body.Close()
	// logrus.Debug(resp.Header)

	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	if err != nil {
		return 0, fmt.Errorf("error parsing Content-Length header: %s", err)
	}

	return uint64(contentLength), nil
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

func NewGrabClient(httpClient *http.Client, randomizeUserAgent bool) *grab.Client {
	var ua string
	if randomizeUserAgent {
		ua = "kerbetor"
	} else {
		ua = uarand.GetRandom()
	}

	if httpClient != nil {
		return &grab.Client{
			UserAgent:  ua,
			HTTPClient: httpClient,
		}
	} else {
		return &grab.Client{
			UserAgent: ua,
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyFromEnvironment,
				},
			},
		}
	}
}

func DownloadFileChunkAsync(sourceUrl string, destinationPath string, startOffset uint64, endOffset uint64, httpClient *http.Client) (chan uint64, chan error) {
	bytesDownloadedCh := make(chan uint64)
	errorCh := make(chan error)

	// init sync http client
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	if startOffset < 0 || endOffset < 0 || startOffset > endOffset {
		errorCh <- fmt.Errorf("invalid range: %d-%d", startOffset, endOffset)
	}

	// init async http client (randomize user agent)
	client := NewGrabClient(httpClient, true)

	// start async chunk download
	downloadReq, _ := grab.NewRequest(destinationPath, sourceUrl)
	downloadReq.HTTPRequest.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startOffset, endOffset))
	resp := client.Do(downloadReq)

	go func() {
		// periodically send number of bytes downloaded
		t := time.NewTicker(DownloadedBytesRefreshRate)
		defer t.Stop()

	DownloadedBytesUpdateLoop:
		for {
			select {
			case <-t.C:
				bytesDownloadedCh <- uint64(resp.BytesComplete())

			case <-resp.Done:
				// download is complete
				break DownloadedBytesUpdateLoop
			}
		}

		if resp.Err() != nil {
			errorCh <- resp.Err()
		}

		close(bytesDownloadedCh)
		close(errorCh)
	}()

	return bytesDownloadedCh, errorCh
}
