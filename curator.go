package main

import "fmt"
import "io/ioutil"
import "os/exec"
import "gopkg.in/yaml.v2"
import "os"
import "bufio"
import "strings"
import "io"
import "path"
import "path/filepath"
import "log"
import "syscall"
import "regexp"

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
	}
	return ""
}

type Process struct {
	Program Program
	Command *exec.Cmd
	State   State
}

var processTable map[string]*Process

// TODO: check output file size and rotate.
func flushStream(stdoutPipe io.ReadCloser, writer *bufio.Writer) {
	buffer := make([]byte, 100, 1000)
	for {
		n, err := stdoutPipe.Read(buffer)
		if err == io.EOF {
			stdoutPipe.Close()
			break
		}
		buffer = buffer[0:n]
		writer.Write(buffer)
		writer.Flush()
	}
}

func spawnProcess(p Program) {
	fmt.Printf("Process: %s\n", p.ProcessName)
	stdoutPath := p.OutputStreams.Stdout.LogFile.Path
	stderrPath := p.OutputStreams.Stderr.LogFile.Path

	fmt.Printf("Writing STDOUT to %s\n", stdoutPath)
	fmt.Printf("Writing STDERR to %s\n", stderrPath)
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

	fmt.Printf("Going to run %s\n", p.Command)
	cmdParts := strings.Split(p.Command, " ")
	fmt.Println(cmdParts)

	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	go flushStream(stdoutPipe, outw)
	go flushStream(stderrPipe, errw)

	proc := new(Process)
	proc.Program = p
	proc.Command = cmd
	proc.State = STARTING

	fmt.Printf("Adding %s to process table\n", p.ProcessName)
	processTable[p.ProcessName] = proc

	log.Printf("Trying to start %s\n", proc.Program.ProcessName)
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
				log.Printf("Exit Status: %d", status.ExitStatus())
			}
			proc.State = STOPPED
		} else {
			log.Fatalf("cmd.Wait: %v", err)
		}
	}

}

func startProgram(programName string) {
	if proc, present := processTable[programName]; present {
		if proc.State == RUNNING {
			log.Print("Not starting a running program!")
		} else {
			log.Printf("Spawning process for %s", proc.Program.ProcessName)
			go spawnProcess(proc.Program)
		}
	}
}

func terminateProgram(programName string) {
	if proc, present := processTable[programName]; present {
		cmd := proc.Command

		if proc.State == STOPPED {
			log.Printf("Nothing to do, process %s already stopped\n", programName)
		} else {
			log.Printf("Terminating program %s\n", programName)
			cmd.Process.Kill()
		}
	}
}

func handleInput(channel chan string, kbd *bufio.Reader) {
	for {
		fmt.Print("CMD: ")
		text, _ := kbd.ReadString('\n')
		text = strings.TrimSpace(text)
		channel <- text
	}
}

func handleConfig(configFile string) {
	var p Program
	var fileData, err = ioutil.ReadFile(configFile)

	if err != nil {
		panic(err)
	}

	err = yaml.Unmarshal(fileData, &p)
	if err != nil {
		panic(err)
	}

	// TODO: handle forced reloading, which would terminate the
	// process and then reload. For now only load new things.
	if _, present := processTable[p.ProcessName]; !present {
		fmt.Printf("Spawning %s\n", p.ProcessName)
		go spawnProcess(p)
	}

}

func loadConfigs(srcDir string) {
	globStr := path.Join(srcDir, "*.yml")
	fmt.Printf("Loading files matching %s\n", globStr)
	configFiles, _ := filepath.Glob(globStr)
	for _, configFile := range configFiles {
		fmt.Printf("Processing %s...\n", configFile)
		handleConfig(configFile)
	}
}

func main() {
	processTable = make(map[string]*Process)
	queue := make(chan string, 1)
	cwd, _ := os.Getwd()
	configDir := cwd
	loadConfigs(configDir)

	reader := bufio.NewReader(os.Stdin)
	go handleInput(queue, reader)

	var status = regexp.MustCompile(`^status\s*$`)
	var reload = regexp.MustCompile(`^reload\s*$`)
	var start = regexp.MustCompile(`^start (\w+?)\s*$`)
	var stop = regexp.MustCompile(`^stop (\w+?)\s*$`)

	for {
		select {
		case val := <-queue:
			switch {
			case status.MatchString(val):
				{
					fmt.Println("Checking status...")
					for k, p := range processTable {
						fmt.Printf("%s: %s\n", k, p.State)
					}
				}
			case reload.MatchString(val):
				{
					fmt.Println("Loading new configs...")
					loadConfigs(configDir)
				}
			case start.MatchString(val):
				{
					result := start.FindStringSubmatch(val)
					program := result[1]
					startProgram(program)
				}
			case stop.MatchString(val):
				{
					result := stop.FindStringSubmatch(val)
					program := result[1]
					terminateProgram(program)
				}
			}
		}
	}

}
