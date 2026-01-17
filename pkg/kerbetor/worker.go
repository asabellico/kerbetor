package kerbetor

import (
	"fmt"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v8"
)

type TorInstanceWorker struct {
	workerIndex uint
	torInstance *TorInstance
	inChunkCh   chan *Chunk
}

const (
	maxChunkRetries = 3
	chunkRetryDelay = 2 * time.Second
)

func (w *TorInstanceWorker) NewChunkProgressBar(chunk *Chunk, p *mpb.Progress) *mpb.Bar {
	return NewProgressBar(p, fmt.Sprintf("[W%d] Chunk #%d ...", w.workerIndex, chunk.index), chunk.endOffset-chunk.startOffset, int(w.workerIndex))
}

func (w *TorInstanceWorker) downloadChunkOnce(chunk *Chunk, bar *mpb.Bar) error {
	var bytesDownloaded chan uint64
	var errors chan error
	if w.torInstance == nil {
		bytesDownloaded, errors = DownloadFileChunkAsync(chunk.remoteUrl, chunk.chunkPath, chunk.startOffset, chunk.endOffset, nil)
	} else {
		bytesDownloaded, errors = w.torInstance.TorDownloadFileChunkAsync(chunk.remoteUrl, chunk.chunkPath, chunk.startOffset, chunk.endOffset)
	}

	var downloadErr error
	for bytesDownloaded != nil || errors != nil {
		select {
		case err, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			if err != nil {
				downloadErr = err
			}
		case recvBytesDownloaded, ok := <-bytesDownloaded:
			if !ok {
				bytesDownloaded = nil
				continue
			}
			chunk.bytesDownloaded = recvBytesDownloaded
			logrus.Debug("Worker #", w.workerIndex, ". Got bytesDownloaded update from channel: ", chunk.bytesDownloaded, " [", humanize.Bytes(uint64(chunk.bytesDownloaded)), "]")
			bar.SetCurrent(int64(chunk.bytesDownloaded))
		}
	}

	return downloadErr
}

func (w *TorInstanceWorker) DownloadWorker(wg *sync.WaitGroup, p *mpb.Progress) {
	defer wg.Done()

	if w.torInstance != nil {
		logrus.Debug("Started worker ", w.workerIndex, " w/ TOR instance ", w.torInstance.port, "...")
	} else {
		logrus.Debug("Started worker ", w.workerIndex, " w/out a TOR instance...")
	}
	for chunk := range w.inChunkCh {
		logrus.Debug(fmt.Sprintf("Worker #%d. Downloading chunk %d (%d-%d) to %s", w.workerIndex, chunk.index, chunk.startOffset, chunk.endOffset, chunk.chunkPath))

		// create chunk progressbar
		bar := w.NewChunkProgressBar(chunk, p)
		if chunk.bytesDownloaded > 0 {
			bar.SetCurrent(int64(chunk.bytesDownloaded))
		}

		var lastErr error
		for attempt := 1; attempt <= maxChunkRetries; attempt++ {
			if attempt > 1 {
				logrus.Warnf("Retrying chunk %d (attempt %d/%d) after error: %v", chunk.index, attempt, maxChunkRetries, lastErr)
				time.Sleep(chunkRetryDelay)
			}

			lastErr = w.downloadChunkOnce(chunk, bar)
			if lastErr == nil {
				logrus.Debug("Chunk #", chunk.index, ". Download completed.")
				chunk.status = ChunkStatusCompleted
				break
			}
		}

		if chunk.status != ChunkStatusCompleted {
			logrus.Error("cannot download chunk: ", lastErr)
			chunk.status = ChunkStatusError
		}

		bar.Abort(true)
	}
}
