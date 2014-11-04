// Sometimes our Go programs need to spawn other, non-Go
// processes. For example, the syntax highlighting on this
// site is [implemented](https://github.com/mmcgrana/gobyexample/blob/master/tools/generate.go)
// by spawning a [`pygmentize`](http://pygments.org/)
// process from a Go program. Let's look at a few examples
// of spawning processes from Go.

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

type OutputStreams struct { 
  Stdout OutputStream
  Stderr OutputStream
}

type OutputStream struct {
  LogFile LogFile `yaml:"log_file"`
}

type LogFile struct { 
  Path string
  Maxbytes int
  Backups int
}


type Program struct {
   Command string
   ProcessName string  `yaml:"process_name"`
   Numprocs int
   NumprocsStart int `yaml:"numprocs_start"`
   Priority int
   Autostart bool
   Autorestart bool
   StartSecs int `yaml:"start_secs"`
   StartRetries int `yaml:"start_retries"`
   ExitCodes []int `yaml:"exit_codes"`
   StopSignal string `yaml:"stop_signal"`
   StopWaitSecs int `yaml:"stop_wait_secs"`
   User string
   RedirectStderr bool `yaml:"redirect_stderr"`
   Environment map[string]string
   Directory string
   ServerURL string `yaml:"server_url"`
   OutputStreams OutputStreams  `yaml:"output_streams"`
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
  State State
}

var processTable map[string]Process 

// TODO: check output file size and rotate.
func flushStream(stdoutPipe io.ReadCloser, writer *bufio.Writer) {
    buffer := make([]byte, 100, 1000)
    for ;; {
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
      //programIn, _ := program.StdinPipe()
      //programOut, _ := program.StdoutPipe()
      //program.Stdout = w
      //program.Stderr = w
      //program.Stdout = os.Stdout
      //program.Stderr = os.Stderr
      //program.Stdout = os.Stdout
      e := cmd.Start()
      if e != nil {
        panic(e)
      }

      go flushStream(stdoutPipe, outw)
      go flushStream(stderrPipe, errw)

      defer cmd.Wait()
      proc := Process{Program: p, Command: cmd, State: RUNNING}
      fmt.Printf("Adding %s to process table\n", p.ProcessName)
      processTable[p.ProcessName] = proc
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

    if (err != nil) {
      panic(err)
    }

    err = yaml.Unmarshal(fileData, &p)
    if err != nil {
      panic(err)
    }

    go spawnProcess(p)
}

func loadConfigs(srcDir string) {
  globStr := path.Join(srcDir, "*.yml")
  fmt.Printf("Loading files matching %s\n", globStr)
  configFiles, _ := filepath.Glob(globStr)
  for _,configFile := range configFiles { 
    fmt.Printf("Loading %s...\n", configFile)
    handleConfig(configFile)
  }
}

func main() {
    processTable = make(map[string]Process)
    queue := make(chan string, 1)
    cwd, _ := os.Getwd()
    loadConfigs(cwd)

    reader := bufio.NewReader(os.Stdin)
    go handleInput(queue, reader)

    for { 
      select {
        case val := <-queue:
          switch val {
            case "STATUS": {
              fmt.Println("Checking status...") 
              for k, p := range processTable  {
                fmt.Printf("%s: %s\n", k, p.State)
              }
            }
          }
      }
    }

}
