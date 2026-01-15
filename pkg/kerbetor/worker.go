package kerbetor

import (
	"fmt"
	"sync"

	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v8"
)

type TorInstanceWorker struct {
	workerIndex uint
	torInstance *TorInstance
	inChunkCh   chan *Chunk
}

func (w *TorInstanceWorker) NewChunkProgressBar(chunk *Chunk, p *mpb.Progress) *mpb.Bar {
	return NewProgressBar(p, fmt.Sprintf("[W%d] Chunk #%d ...", w.workerIndex, chunk.index), chunk.endOffset-chunk.startOffset, int(w.workerIndex))
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

		// start chunk download
		var bytesDownloaded chan uint64
		var errors chan error
		if w.torInstance == nil {
			bytesDownloaded, errors = DownloadFileChunkAsync(chunk.remoteUrl, chunk.chunkPath, chunk.startOffset, chunk.endOffset, nil)
		} else {
			bytesDownloaded, errors = w.torInstance.TorDownloadFileChunkAsync(chunk.remoteUrl, chunk.chunkPath, chunk.startOffset, chunk.endOffset)
		}
	chunkDownloadLoop:
		for {
			select {
			case err, ok := <-errors:
				if !ok {
					errors = nil
					continue
				}
				if err != nil {
					logrus.Error("cannot download chunk: ", err)
					chunk.status = ChunkStatusError
					break chunkDownloadLoop
				}
			case recvBytesDownloaded, ok := <-bytesDownloaded:
				if !ok {
					bytesDownloaded = nil
					logrus.Debug("Chunk #", chunk.index, ". Download completed.")
					chunk.status = ChunkStatusCompleted
					break chunkDownloadLoop
				}
				logrus.Debug("Worker #", w.workerIndex, ". Got bytesDownloaded update from channel: ", chunk.bytesDownloaded, " [", humanize.Bytes(uint64(chunk.bytesDownloaded)), "]")
				chunk.bytesDownloaded = recvBytesDownloaded
				bar.SetCurrent(int64(chunk.bytesDownloaded))
			}
		}

		bar.Abort(true)
	}
}
