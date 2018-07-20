package main

import (
	"bufio"
	"flag"
	"fmt"
	"harmony-benchmark/aws-experiment-launch/experiment/utils"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type commanderSetting struct {
	ip        string
	port      string
	configURL string
	configs   [][]string
}

type sessionInfo struct {
	id           string
	uploadFolder string
}

var (
	setting commanderSetting
	session sessionInfo
)

const (
	DistributionFileName = "distribution_config.txt"
)

func readConfigFile() [][]string {
	if err := utils.DownloadFile(DistributionFileName, setting.configURL); err != nil {
		panic(err)
	}

	if result, err := utils.ReadDistributionConfig(DistributionFileName); err != nil {
		panic(err)
	} else {
		return result
	}
}

func handleCommand(command string) {
	args := strings.Split(command, " ")
	if len(args) <= 0 {
		return
	}

	switch cmd := args[0]; cmd {
	case "config":
		setting.configs = readConfigFile()
		if setting.configs != nil {
			log.Printf("The loaded config has %v nodes\n", len(setting.configs))
		} else {
			log.Println("Failed to read config file")
		}
	case "init":
		session.id = time.Now().Format("150405-20060102")
		// create upload folder
		session.uploadFolder = fmt.Sprintf("upload/%s", session.id)
		err := os.MkdirAll(session.uploadFolder, os.ModePerm)
		if err != nil {
			log.Println("Failed to create upload folder", session.uploadFolder)
			return
		}
		log.Println("New session", session.id)

		dictateNodes(fmt.Sprintf("init %v %v %v %v", setting.ip, setting.port, setting.configURL, session.id))
	case "ping", "kill", "log", "log2":
		dictateNodes(command)
	default:
		log.Println("Unknown command")
	}
}

func config(ip string, port string, configURL string) {
	setting.ip = ip
	setting.port = port
	setting.configURL = configURL
}

func dictateNodes(command string) {
	resultChan := make(chan int)
	for _, config := range setting.configs {
		ip := config[0]
		port := "1" + config[1] // the port number of solider is "1" + node port
		addr := strings.Join([]string{ip, port}, ":")

		go func(resultChan chan int) {
			resultChan <- dictateNode(addr, command)
		}(resultChan)
	}
	count := len(setting.configs)
	res := 0
	for ; count > 0; count-- {
		res += <-resultChan
	}

	log.Printf("Finished %s with %v nodes\n", command, res)
}

func dictateNode(addr string, command string) int {
	// creates client
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		log.Println(err)
		return 0
	}
	defer conn.Close()

	// send command
	_, err = conn.Write([]byte(command))
	if err != nil {
		log.Printf("Failed to send command to %s", addr)
		return 0
	}
	log.Printf("Send \"%s\" to %s", command, addr)

	// read response
	buff := make([]byte, 1024)
	if n, err := conn.Read(buff); err == nil {
		log.Printf("Receive from %s: %s", addr, buff[:n])
		return 1
	}
	return 0
}

func handleUploadRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// reject non-post requests
		jsonResponse(w, http.StatusBadRequest, "Only post request is accepted.")
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		dst, err := os.Create(fmt.Sprintf("%s/%s", session.uploadFolder, part.FileName()))
		log.Println(part.FileName())
		if err != nil {
			jsonResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, part); err != nil {
			jsonResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
}

func jsonResponse(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprint(w, message)
	log.Println(message)
}

func serve() {
	http.HandleFunc("/upload", handleUploadRequest)
	err := http.ListenAndServe(":"+setting.port, nil)
	if err != nil {
		log.Fatalf("Failed to setup server! Error: %s", err.Error())
	}
	log.Printf("Start to host upload endpoint at http://%s:%s/upload\n", setting.ip, setting.port)
}

func main() {
	ip := flag.String("ip", "127.0.0.1", "The ip of commander, i.e. this machine")
	port := flag.String("port", "8080", "The port which the commander uses to communicate with soldiers")
	configURL := flag.String("config_url", "https://s3-us-west-2.amazonaws.com/unique-bucket-bin/distribution_config.txt", "The config URL")
	flag.Parse()

	config(*ip, *port, *configURL)

	go serve()

	scanner := bufio.NewScanner(os.Stdin)
	for true {
		log.Printf("Listening to Your Command:")
		if !scanner.Scan() {
			break
		}
		handleCommand(scanner.Text())
	}
}
