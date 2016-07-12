package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"bytes"
	"github.com/gorilla/mux"
	"text/template"
)

type OutputStreams struct {
	Stdout OutputStream
	Stderr OutputStream
}

type OutputStream struct {
	LogFile LogFile `yaml:"log_file"`
}

type LogFile struct {
	Path     string
	Maxbytes int
	Backups  int
}

type Program struct {
	Name           string
	Command        string
	ProcessName    string `yaml:"process_name"`
	Numprocs       int
	NumprocsStart  int `yaml:"numprocs_start"`
	Priority       int
	Autostart      bool
	Autorestart    bool
	StartSecs      int    `yaml:"start_secs"`
	StartRetries   int    `yaml:"start_retries"`
	ExitCodes      []int  `yaml:"exit_codes"`
	StopSignal     string `yaml:"stop_signal"`
	StopWaitSecs   int    `yaml:"stop_wait_secs"`
	User           string
	RedirectStderr bool `yaml:"redirect_stderr"`
	Environment    map[string]string
	Directory      string
	ServerURL      string        `yaml:"server_url"`
	OutputStreams  OutputStreams `yaml:"output_streams"`
}

type State int

const (
	STOPPED State = iota
	STARTING
	RUNNING
	STOPPING
	FAILED
)

func (s State) String() string {
	switch s {
	case STOPPED:
		return "STOPPED"
	case STARTING:
		return "STARTING"
	case RUNNING:
		return "RUNNING"
	case STOPPING:
		return "STOPPING"
	case FAILED:
		return "FAILED"
	}
	return ""
}

type ManagedProcess struct {
	Title         string
	Command       string
	Process       *exec.Cmd
	State         State
	OutputStreams OutputStreams
	ProgramName   string
	StartCount    int
	Terminated    bool
}

type ProcessMetadata struct {
	ProcessNumber int
	ProcessName   string
}

var processTable map[string]*ManagedProcess
var programTable map[string]*Program

func makeReadChan(r io.Reader, bufSize int) (chan []byte, chan error) {
	read := make(chan []byte)
	errc := make(chan error, 1)
	go func() {
		for {
			b := make([]byte, bufSize)
			n, err := r.Read(b)
			if err != nil {
				fmt.Print("Error!")
				close(read)
				errc <- err
				return
			}
			if n > 0 {
				fmt.Print("Read %d bytes\n", n)
				read <- b[0:n]
			}
		}
	}()
	return read, errc
}

// TODO: check output file size and rotate.
func flushStream(stdoutPipe *BlockReadWriter, writer *bufio.Writer) {
	buffer := make([]byte, 100, 1000)

	for {
		n, err := stdoutPipe.Read(buffer)
		buffer = buffer[0:n]
		writer.Write(buffer)
		writer.Flush()

		if err == io.EOF {
			//stdoutPipe.Close()
			fmt.Printf("EOF\n")
			break
		}
	}
}

func spawnProcess(proc ManagedProcess) {
	log.Printf("[%s] Spawning process...\n", proc.Title)
	program := programTable[proc.ProgramName]

	for i := 0; i < program.StartRetries && !proc.Terminated; i++ {
		proc.StartCount++
		log.Printf("[%s] Running %d/%d times...\n", proc.Title, i, program.StartRetries)

		stdoutPath := proc.OutputStreams.Stdout.LogFile.Path
		stderrPath := proc.OutputStreams.Stderr.LogFile.Path

		log.Printf("[%s] Writing STDOUT to %s\n", proc.Title, stdoutPath)
		log.Printf("[%s] Writing STDERR to %s\n", proc.Title, stderrPath)
		outf, err := os.Create(stdoutPath)
		if err != nil {
			panic(err)
		}

		errf, err := os.Create(stderrPath)
		if err != nil {
			panic(err)
		}

		outw := bufio.NewWriter(outf)
		errw := bufio.NewWriter(errf)

		log.Printf("[%s] Command: %s\n", proc.Title, proc.Command)
		cmdParts := strings.Split(proc.Command, " ")

		cmd := exec.Command(cmdParts[0], cmdParts[1:]...)

		stdoutPipe := NewBlockReadWriter()
		stderrPipe := NewBlockReadWriter()

		cmd.Stdout = stdoutPipe
		cmd.Stderr = stderrPipe

		go flushStream(stdoutPipe, outw)
		go flushStream(stderrPipe, errw)

		proc.State = STARTING
		proc.Process = cmd

		log.Printf("[%s] Adding '%s' to process table\n", proc.Title, proc.Title)
		processTable[proc.Title] = &proc

		e := cmd.Start()
		if e != nil {
			panic(e)
		}

		proc.State = RUNNING

		if err := cmd.Wait(); err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// The program has exited with an exit code != 0

				// This works on both Unix and Windows. Although package
				// syscall is generally platform dependent, WaitStatus is
				// defined for both Unix and Windows and in both cases has
				// an ExitStatus() method with the same signature.
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					log.Printf("[%s] Exit Status: %d", proc.Title, status.ExitStatus())
				}
				proc.State = STOPPED
			} else {
				log.Fatalf("[%s] cmd.Wait: %v", proc.Title, err)
			}
		} else {
			log.Printf("[%s] Exited gracefully.", proc.Title)
			proc.State = STOPPED
		}

	}

	if proc.StartCount == program.StartRetries {
		log.Printf("[%s] Tried to startup %d times, marking as FAILED", proc.Title, program.StartRetries)
		proc.State = FAILED
	}

	if proc.Terminated {
		log.Printf("[%s] Terminated on command", proc.Title)
		proc.State = STOPPED
	}

}

func startProgram(programName string) {
	if proc, present := processTable[programName]; present {
		if proc.State == RUNNING {
			log.Print("[curator] Not starting a running program!")
		} else {
			proc.Terminated = false
			proc.StartCount = 0
			log.Printf("[curator] Spawning process for %s", proc.Title)
			go spawnProcess(*proc)
		}
	}
}

func terminateProgram(programName string) {
	if proc, present := processTable[programName]; present {
		cmd := proc.Process

		if proc.State == STOPPED {
			log.Printf("[curator] Nothing to do, process %s already stopped\n", programName)
		} else {
			log.Printf("[curator] Terminating program %s\n", programName)
			proc.State = STOPPED
			proc.Terminated = true
			cmd.Process.Kill()
			proc.Process = nil
		}
	}
}

func handleConfig(configFile string) {
	var p *Program = new(Program)
	var fileData, err = ioutil.ReadFile(configFile)

	if err != nil {
		panic(err)
	}

	err = yaml.Unmarshal(fileData, p)
	if err != nil {
		fmt.Printf("Error parsing YAML file '%s': %s", configFile, err)
		return
	}

	p.Name = strings.Replace(configFile, ".yml", "", 1)
	programTable[p.Name] = p

	if p.Autostart {
		for i := p.NumprocsStart; i < p.Numprocs+p.NumprocsStart; i++ {
			pm := ProcessMetadata{
				ProcessNumber: i,
				ProcessName:   p.ProcessName,
			}

			processTitle, _ := injectProcessMetadata(p.ProcessName, &pm)
			commandString, _ := injectProcessMetadata(p.Command, &pm)
			stderrLogFile, _ := injectProcessMetadata(p.OutputStreams.Stderr.LogFile.Path, &pm)
			stdoutLogFile, _ := injectProcessMetadata(p.OutputStreams.Stdout.LogFile.Path, &pm)

			newOutputStreams := p.OutputStreams
			newOutputStreams.Stderr.LogFile.Path = stderrLogFile
			newOutputStreams.Stdout.LogFile.Path = stdoutLogFile

			// TODO: handle forced reloading, which would terminate the
			// process and then reload. For now only load new things.
			if _, present := processTable[processTitle]; !present {
				log.Printf("[curator] Spawning %s\n", processTitle)
				proc := ManagedProcess{
					ProgramName:   p.Name,
					Title:         processTitle,
					Command:       commandString,
					OutputStreams: newOutputStreams,
					Terminated:    false,
					StartCount:    0,
				}
				go spawnProcess(proc)
			}
		}
	}

}

func injectProcessMetadata(templateData string, metadata *ProcessMetadata) (string, error) {
	var doc bytes.Buffer
	tmpl, err := template.New("processname").Parse(templateData)

	if err != nil {
		return "", fmt.Errorf("Unable to process '%s': %s", templateData, err)
	}

	tmpl.Execute(&doc, *metadata)
	return doc.String(), nil
}

func loadConfigs(srcDir string) {
	globStr := path.Join(srcDir, "*.yml")
	log.Printf("[curator] Loading files matching %s\n", globStr)
	configFiles, _ := filepath.Glob(globStr)
	for _, configFile := range configFiles {
		log.Printf("[curator] Processing %s...\n", configFile)
		handleConfig(configFile)
	}
}

func Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	processes := make(map[string]string)
	for k, p := range processTable {
		processes[k] = fmt.Sprintf("%s", p.State)
	}
	json.NewEncoder(w).Encode(processes)
}

func ShowProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processName := vars["processName"]
	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(processTable[processName])
}

func StopProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processName := vars["processName"]
	terminateProgram(processName)
	w.Header().Add("Content-Type", "application/json")
	fmt.Fprint(w, "OK")
}

func StartProcess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	processName := vars["processName"]
	startProgram(processName)
	w.Header().Add("Content-Type", "application/json")
	fmt.Fprint(w, "OK")
}

func ReloadConfigs(w http.ResponseWriter, r *http.Request) {
	cwd, _ := os.Getwd()
	configDir := cwd
	loadConfigs(configDir)
	w.Header().Add("Content-Type", "application/json")
	fmt.Fprint(w, "OK")
}

func main() {

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	processTable = make(map[string]*ManagedProcess)
	programTable = make(map[string]*Program)
	cwd, _ := os.Getwd()
	configDir := cwd
	loadConfigs(configDir)

	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", Index)
	router.HandleFunc("/reload", ReloadConfigs)
	router.HandleFunc("/processes/{processName}", ShowProcess)
	router.HandleFunc("/processes/{processName}/stop", StopProcess)
	router.HandleFunc("/processes/{processName}/start", StartProcess)

	log.Fatal(http.ListenAndServe(":8080", router))
}
