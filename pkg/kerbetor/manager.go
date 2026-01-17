package kerbetor

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v8"
)

func ConcurrentFileDownload(remoteUrl string, destinationPath string, chunkSize uint64, maxConcurrentDownloads uint, numTorCircuits uint, chunkCount uint) error {
	// create tor circuits
	var circuits []*TorInstance
	var mainHttpClient *http.Client

	if numTorCircuits > 0 {
		logrus.Info("Creating TOR circuits...")
		var err error
		circuits, err = CreateTorCircuits(numTorCircuits)
		if err != nil {
			return fmt.Errorf("cannot create tor circuits. %s", err)
		}

		for _, circuit := range circuits {
			defer circuit.Close()
		}

		mainHttpClient = circuits[0].GetTorHttpClient()
	} else {
		mainHttpClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	}

	// get remote file size
	logrus.Debug("Getting remote file size ...")
	fileSize, err := GetRemoteFileSize(remoteUrl, mainHttpClient)
	if err != nil {
		return fmt.Errorf("cannot get remote file size. %s", err)
	}
	logrus.Info("Remote file size: ", humanize.Bytes(uint64(fileSize)))

	if chunkCount > 0 {
		chunkSize = (fileSize + uint64(chunkCount) - 1) / uint64(chunkCount)
		logrus.Info("Computed chunk size: ", humanize.Bytes(uint64(chunkSize)))
	} else if chunkSize == 0 {
		return fmt.Errorf("chunk size cannot be 0")
	}

	// create chunk controller
	logrus.Debug("Creating chunk controller...")
	// create work dir
	workDir := destinationPath + ".ktor"
	chunkController, err := NewChunkController(remoteUrl, workDir, fileSize, chunkSize)
	if err != nil {
		return fmt.Errorf("cannot create chunk controller. %s", err)
	}

	// create download workers
	var workersWG sync.WaitGroup

	// create progress bar manager
	progressbars := mpb.New(mpb.WithWidth(64), mpb.WithWaitGroup(&workersWG), mpb.WithRefreshRate(180*time.Millisecond))
	mainBar := NewProgressBar(progressbars, "#### Total ...", fileSize, math.MaxInt)
	go func() {
		for {
			mainBar.SetCurrent(int64(chunkController.GetDownloadedSize()))
			time.Sleep(DownloadedBytesRefreshRate)
		}
	}()

	logrus.Debug("Creating ", maxConcurrentDownloads, " download workers...")
	workers := make([]*TorInstanceWorker, maxConcurrentDownloads)
	var i uint
	for i = 0; i < maxConcurrentDownloads; i++ {
		if numTorCircuits > 0 {
			workers[i] = &TorInstanceWorker{workerIndex: i, torInstance: circuits[i%numTorCircuits], inChunkCh: make(chan *Chunk)}
		} else {
			workers[i] = &TorInstanceWorker{workerIndex: i, torInstance: nil, inChunkCh: make(chan *Chunk)}
		}

		workersWG.Add(1)
		go workers[i].DownloadWorker(&workersWG, progressbars)
	}

	logrus.Debug("Sending chunks to workers ...")
	// send chunks to workers
	for i = 0; ; i++ {
		workerIndex := i % maxConcurrentDownloads
		chunk := chunkController.GetNextEmptyChunk()
		if chunk == nil {
			break
		}
		workers[workerIndex].inChunkCh <- chunk
	}

	// close channels and wait for workers to finish
	logrus.Debug("Closing channels ...")
	for i = 0; i < maxConcurrentDownloads; i++ {
		close(workers[i].inChunkCh)
	}

	workersWG.Wait()
	logrus.Debug("Waiting for workers to finish ...")

	flag := false
	for _, chunk := range *chunkController.chunks {
		logrus.Debug("Chunk: ", chunk.chunkPath, " status: ", chunk.status)
		if chunk.status != ChunkStatusCompleted {
			logrus.Errorf("Chunk %s was not downloaded", chunk.chunkPath)
			flag = true
		}
	}
	if flag {
		return fmt.Errorf("some chunks were not downloaded")
	}

	mainBar.Abort(true)
	logrus.Info("Merging chunks ...")
	_, err = chunkController.MergeChunks(destinationPath)
	if err != nil {
		return fmt.Errorf("cannot merge chunks. %s", err)
	}
	return nil
}
