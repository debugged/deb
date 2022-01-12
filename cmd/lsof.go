package cmd

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type Process struct {
	PID   string
	Name  string
	Count int
}

type ByCount []Process

func (s ByCount) Len() int {
	return len(s)
}

func (s ByCount) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByCount) Less(i, j int) bool {
	return s[i].Count < s[j].Count
}

// lsofCmd represents the lsof command
var lsofCmd = &cobra.Command{
	Use:   "lsof",
	Short: "Show process and file descriptors count",
	Run: func(cmd *cobra.Command, args []string) {
		if runtime.GOOS == "windows" {
			log.Fatal("lsof not supported on windows")
		}

		c := exec.Command("lsof")

		stderr, err := c.StdoutPipe()
		if err != nil {
			log.Fatal(err)
		}
		c.Start()

		processMap := make(map[string]*Process)
		scanner := bufio.NewScanner(stderr)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			m := scanner.Text()
			f := strings.Fields(m)

			// Skip header
			if len(f) == 3 {
				continue
			}

			pid := f[1]
			name := f[0]
			process, ok := processMap[pid]
			if !ok {
				process = &Process{
					PID:  pid,
					Name: name,
				}
				processMap[pid] = process
			}
			process.Count++
		}
		c.Wait()

		processes := make([]Process, len(processMap))
		i := 0
		for _, p := range processMap {
			processes[i] = *p
			i++
		}
		sort.Sort(ByCount(processes))

		data := make([][]string, len(processes))
		total := 0
		for i, p := range processes {
			data[i] = []string{p.PID, p.Name, strconv.Itoa(p.Count)}
			total += p.Count
		}

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"PID", "Name", "Descriptors"})
		table.SetFooter([]string{"", "Total", strconv.Itoa(total)})
		table.SetFooterAlignment(tablewriter.ALIGN_RIGHT)
		table.SetBorder(false)
		table.AppendBulk(data)
		table.Render()
	},
}

func init() {
	rootCmd.AddCommand(lsofCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// lsofCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// lsofCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
