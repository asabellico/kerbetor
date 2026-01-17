package kerbetor

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

type ChunkStatus uint8

const (
	ChunkStatusNotStarted ChunkStatus = iota
	ChunkStatusInProgress
	ChunkStatusCompleted
	ChunkStatusError
)

type Chunk struct {
	remoteUrl   string
	startOffset uint64
	endOffset   uint64

	index           int
	chunkPath       string
	status          ChunkStatus
	bytesDownloaded uint64
}

type ChunkController struct {
	workPath  string
	fileSize  uint64
	chunkSize uint64
	chunks    *[]*Chunk
}

func CheckChunksMetadata(remoteUrl string, workPath string, fileSize uint64, chunkSize uint64) (bool, error) {
	// check file metadata.ktor if remoteUrl, fileSize and chunkSize are the same
	// if not, returns false and error

	// if metadata.ktor does not exist, create it
	if _, err := os.Stat(workPath + "/metadata.ktor"); os.IsNotExist(err) {
		metadataFile, err := os.Create(workPath + "/metadata.ktor")
		if err != nil {
			return false, fmt.Errorf("cannot create file %s/metadata.ktor: %s", workPath, err)
		}
		defer metadataFile.Close()

		_, err = metadataFile.WriteString(remoteUrl + "\n" + strconv.FormatInt(int64(fileSize), 10) + "\n" + strconv.FormatInt(int64(chunkSize), 10))
		if err != nil {
			return false, fmt.Errorf("cannot write to file %s/metadata.ktor: %s", workPath, err)
		}
		return true, nil
	}

	// read metadata.ktor file
	metadataFile, err := os.Open(workPath + "/metadata.ktor")
	if err != nil {
		return false, fmt.Errorf("cannot open file %s/metadata.ktor: %s", workPath, err)
	}
	defer metadataFile.Close()

	metadataFileContent, err := ioutil.ReadAll(metadataFile)
	if err != nil {
		return false, fmt.Errorf("cannot read file %s/metadata.ktor: %s", workPath, err)
	}

	// check if remoteUrl, fileSize and chunkSize are the same
	// if not, return false
	metadata := string(metadataFileContent)
	metadataArray := strings.Split(metadata, "\n")
	if metadataArray[0] != remoteUrl {
		return false, fmt.Errorf("remote URL is different")
	}
	if metadataArray[1] != strconv.FormatInt(int64(fileSize), 10) {
		return false, fmt.Errorf("file size is different")
	}
	if metadataArray[2] != strconv.FormatInt(int64(chunkSize), 10) {
		return false, fmt.Errorf("chunk size is different")
	}
	return true, nil
}

func GenerateChunks(fileSize uint64, chunkSize uint64, workPath string, remoteUrl string) *[]*Chunk {
	var chunks []*Chunk
	if fileSize == 0 {
		return &chunks
	}
	var startOffset uint64 = 0
	var endOffset uint64 = uint64(math.Min(float64(chunkSize), float64(fileSize))) - 1

	for idx := 0; ; idx++ {
		chunks = append(chunks, &Chunk{
			remoteUrl:   remoteUrl,
			startOffset: startOffset,
			endOffset:   endOffset,
			chunkPath:   fmt.Sprintf("%s/%d.part", workPath, idx),
			index:       idx,
		})
		startOffset = endOffset + 1
		if startOffset >= fileSize {
			break
		}
		endOffset = startOffset + chunkSize - 1
		if endOffset >= fileSize {
			endOffset = fileSize - 1
			chunks = append(chunks, &Chunk{
				remoteUrl:   remoteUrl,
				startOffset: startOffset,
				endOffset:   endOffset,
				chunkPath:   fmt.Sprintf("%s/%d.part", workPath, idx+1),
				index:       idx + 1,
			})
			break
		}
	}

	logrus.Debug("Generated chunks: " + strconv.Itoa(len(chunks)))
	return &chunks
}

func NewChunkController(remoteUrl string, workPath string, fileSize uint64, chunkSize uint64) (*ChunkController, error) {
	// if workPath directory do not exist, create it
	if _, err := os.Stat(workPath); os.IsNotExist(err) {
		err := os.Mkdir(workPath, os.ModePerm)
		if err != nil {
			return nil, fmt.Errorf("cannot create directory %s: %s", workPath, err)
		}
	}

	// check if metadata.ktor file is correct
	metadataCheck, err := CheckChunksMetadata(remoteUrl, workPath, fileSize, chunkSize)
	if metadataCheck == false {
		return nil, fmt.Errorf("error checking metadata: %s", err)
	}

	chunks := GenerateChunks(fileSize, chunkSize, workPath, remoteUrl)
	for idx, chunk := range *chunks {

		if exists, _ := FileExists(chunk.chunkPath); !exists {
			// chunk file does not exist
			chunk.status = ChunkStatusNotStarted
		} else {
			// chunk file exists
			// check if chunk file size is correct
			chunkFileSize, err := GetFileSize(chunk.chunkPath)
			if err != nil {
				return nil, fmt.Errorf("cannot get file size of %s: %s", chunk.chunkPath, err)
			}
			expectedSize := chunk.endOffset - chunk.startOffset + 1
			if chunkFileSize == expectedSize {
				// chunk file size is correct
				chunk.status = ChunkStatusCompleted
			} else if chunkFileSize > 0 && chunkFileSize < expectedSize {
				// chunk file is partially downloaded
				(*chunks)[idx].status = ChunkStatusNotStarted
				(*chunks)[idx].bytesDownloaded = chunkFileSize
			} else {
				// chunk file size is not correct
				(*chunks)[idx].status = ChunkStatusNotStarted
			}
		}
	}

	return &ChunkController{
		workPath:  workPath,
		fileSize:  fileSize,
		chunkSize: chunkSize,
		chunks:    chunks,
	}, nil
}

func (c *ChunkController) GetNextEmptyChunk() *Chunk {
	for _, chunk := range *c.chunks {
		if chunk.status == ChunkStatusNotStarted {
			chunk.status = ChunkStatusInProgress
			return chunk
		}
	}
	return nil
}

func (c *ChunkController) GetDownloadedSize() uint64 {
	var downloadedSize uint64 = 0
	for _, chunk := range *c.chunks {
		switch chunk.status {
		case ChunkStatusNotStarted:
			downloadedSize += chunk.bytesDownloaded
		case ChunkStatusCompleted:
			downloadedSize += chunk.endOffset - chunk.startOffset + 1
		case ChunkStatusInProgress:
			downloadedSize += chunk.bytesDownloaded
		}
	}

	return downloadedSize
}

func (c *ChunkController) MergeChunks(destinationPath string) (bool, error) {
	for _, chunk := range *c.chunks {
		if chunk.status != ChunkStatusCompleted {
			return false, fmt.Errorf("cannot merge chunks, chunk %s is not downloaded", chunk.chunkPath)
		}
	}

	// open destination file
	destinationFile, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return false, fmt.Errorf("cannot open file %s: %s", destinationPath, err)
	}
	defer destinationFile.Close()

	for _, chunk := range *c.chunks {
		// open chunk file
		chunkFile, err := os.Open(chunk.chunkPath)
		if err != nil {
			return false, fmt.Errorf("cannot open file %s: %s", chunk.chunkPath, err)
		}

		// copy chunk file to destination file
		_, err = io.Copy(destinationFile, chunkFile)
		if err != nil {
			return false, fmt.Errorf("cannot copy file %s to %s: %s", chunk.chunkPath, destinationPath, err)
		}

		chunkFile.Close()
		// os.Remove(chunk.chunkPath)
	}

	os.RemoveAll(c.workPath)
	return true, nil
}
