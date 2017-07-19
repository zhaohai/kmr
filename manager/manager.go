package manager

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"path"
	"regexp"

	"github.com/naturali/kmr/job"
	"github.com/naturali/kmr/util/log"

	"k8s.io/client-go/kubernetes"
)

var (
	uiDir string
)

// KmrManagerWeb KMR Web server
type KmrManagerWeb struct {
	Namespace   string
	ConfigStore string

	k8sclient *kubernetes.Clientset

	// HTTP
	server  *http.Server
	handler *http.ServeMux

	configs map[string]string

	nameRegex *regexp.Regexp
	uiDir     string
}

// NewKmrManagerWeb New web server
func NewKmrManagerWeb(namespace, configStore string, k8sClient *kubernetes.Clientset) *KmrManagerWeb {
	nameRegex, _ := regexp.Compile("^[a-z]([-a-z0-9]*[a-z0-9])?$")

	server := KmrManagerWeb{
		Namespace:   namespace,
		ConfigStore: configStore,
		k8sclient:   k8sClient,
		nameRegex:   nameRegex,
	}
	curDir, _ := Getcurdir()
	server.uiDir = path.Join(curDir, "ui")

	server.configs = LoadConfigFromDirectory(configStore)
	return &server
}

// LoadConfigFromDirectory load config from configStore directory
func LoadConfigFromDirectory(directory string) map[string]string {
	nameRegex, _ := regexp.Compile("^[a-z]([-a-z0-9]*[a-z0-9])?\\.json$")
	result := make(map[string]string)

	files, _ := ioutil.ReadDir(directory)
	for _, file := range files {
		if !nameRegex.Match([]byte(file.Name())) {
			continue
		}
		filename := file.Name()
		name := filename[:len(filename)-len(".json")]
		raw, err := ioutil.ReadFile(path.Join(directory, filename))
		if err != nil {
			log.Infof("Can't read %s: %v", path.Join(directory, filename), err)
			continue
		}
		var jobDescription job.JobDescription
		err = json.Unmarshal(raw, &jobDescription)
		if err != nil {
			log.Infof("Can't parse description file: %v", err)
			continue
		}
		var rebuiltJSON []byte
		rebuiltJSON, _ = json.Marshal(jobDescription)
		result[name] = string(rebuiltJSON)
	}
	return result
}

// Serve start server
func (server *KmrManagerWeb) Serve(port *string) {
	handler := http.ServeMux{}
	server.handler = &handler
	server.server = &http.Server{Addr: *port, Handler: server.handler}

	// Routing
	handler.HandleFunc("/", server.IndexHandler)
	handler.HandleFunc("/hello", server.HelloHandler)
	handler.HandleFunc("/api/v1/create", server.CreateJobHandler)

	// Start server
	log.Infof("Listening at %s", *port)
	server.server.ListenAndServe()
	select {}
}

func (server *KmrManagerWeb) IndexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, path.Join(server.uiDir, "index.html"))
}

func (server *KmrManagerWeb) HelloHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello!")
}

type CreateJobData struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	// We can use following regexp to parse commandline
	// "(\\.|[^"])*?"|'(\\.|[^'])*?'|\S+
	Command []string           `json:"command"`
	JobDesc job.JobDescription `json:"jobDesc"`
}

func (server *KmrManagerWeb) CreateJobHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Can't read request body: %v", err))
		return
	}
	var body CreateJobData
	err = json.Unmarshal(raw, &body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, fmt.Sprintf("Can't decode json: %v", err))
		return
	}

	service := server.NewMasterService(body.Name)
	pod := server.NewMasterPod(body.Name, body.Image, body.Command)
	err = server.StartMaster(pod, service)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, fmt.Sprintf("Can't start master: %v", err))
		return
	}
}