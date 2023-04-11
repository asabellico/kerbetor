package kerbetor

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

type TorInstance struct {
	cmd  *exec.Cmd
	port int
}

// look for a free port to listen on
func GetFreePort() (int, error) {
	// look for a free port to listen on
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("cannot find free port to listen on: %s", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func CreateTorCircuits(numTorCircuits uint) ([]*TorInstance, error) {
	outTorInstances := make(chan *TorInstance, numTorCircuits)
	outErrors := make(chan error, numTorCircuits)

	var wg sync.WaitGroup

	var i uint
	for i = 0; i < numTorCircuits; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := CreateTorCircuit()
			if e != nil {
				outErrors <- e
			}

			outTorInstances <- c
		}()
	}

	wg.Wait()

	close(outTorInstances)
	close(outErrors)

	// extract tor circuits from channel
	torCircuits := make([]*TorInstance, 0, numTorCircuits)
	for newCircuit := range outTorInstances {
		torCircuits = append(torCircuits, newCircuit)
	}

	if len(outErrors) > 0 {
		var errMsgs []string
		for err := range outErrors {
			errMsgs = append(errMsgs, err.Error())
		}

		// close all circuits before returning error
		for _, c := range torCircuits {
			c.Close()
		}

		return nil, fmt.Errorf("failed to create Tor circuit(s): %v", strings.Join(errMsgs, ", "))
	}

	return torCircuits, nil
}

func CreateTorCircuit() (*TorInstance, error) {
	// check if tor executable is available in PATH
	_, err := exec.LookPath("tor")
	if err != nil {
		return nil, fmt.Errorf("tor executable not found in PATH")
	}

	// look for a free port to listen on
	listenPort, err := GetFreePort()
	if err != nil {
		return nil, fmt.Errorf("cannot find free port to listen on: %s", err)
	}

	// generate temp directory for tor data
	torDataDir, err := ioutil.TempDir("", "ktor-tor-data")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary directory for tor data: %s", err)
	}
	// start tor with SOCKS proxy on localhost:listenPort
	torCmd := exec.Command("tor", "--SOCKSPort", fmt.Sprintf("localhost:%d", listenPort), "--DataDirectory", torDataDir)
	torOut, err := torCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Cannot create pipe to tor stdout. %s", err)
	}

	torCmd.Start()

	scanner := bufio.NewScanner(torOut)
	for scanner.Scan() {
		lastScannedLine := scanner.Text()
		logrus.Debug(fmt.Sprintf("[TorInstance %d] %s", listenPort, lastScannedLine))

		// check if last scanned line contains "Bootstrapped 100%"
		if strings.Contains(lastScannedLine, "Bootstrapped 100%") {
			logrus.Debug("[TorInstance ", listenPort, "] Tor circuit bootstrap completed. Listening on port ", listenPort)
			break
		}
	}

	// continue to log tor output as debug messages
	go func() {
		for scanner.Scan() {
			logrus.Debug(fmt.Sprintf("[TorInstance %d] %s", listenPort, scanner.Text()))
		}
	}()

	torInstance := &TorInstance{cmd: torCmd, port: listenPort}
	return torInstance, nil
}

func (t *TorInstance) Close() {
	t.cmd.Process.Kill()
}

func (t *TorInstance) GetTorHttpClient() *http.Client {
	proxyUrl, _ := url.Parse(fmt.Sprintf("socks5://localhost:%d", t.port))
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	return client
}

func (t *TorInstance) TorGetRemoteFileSize(sourceUrl string) (uint64, error) {
	return GetRemoteFileSize(sourceUrl, t.GetTorHttpClient())
}

func (t *TorInstance) TorDownloadFileChunk(sourceUrl string, destinationPath string, startOffset int64, endOffset int64) (int64, error) {
	return DownloadFileChunk(sourceUrl, destinationPath, startOffset, endOffset, t.GetTorHttpClient())
}

func (t *TorInstance) TorDownloadFileChunkAsync(sourceUrl string, destinationPath string, startOffset uint64, endOffset uint64) (chan uint64, chan error) {
	return DownloadFileChunkAsync(sourceUrl, destinationPath, startOffset, endOffset, t.GetTorHttpClient())
}
