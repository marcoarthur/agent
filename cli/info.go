package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	exc "github.com/subutai-io/agent/lib/exec"

	"github.com/influxdata/influxdb/client/v2"
	"github.com/subutai-io/agent/config"
	"github.com/subutai-io/agent/lib/container"
	"github.com/subutai-io/agent/lib/fs"
	"github.com/subutai-io/agent/lib/gpg"
	"github.com/subutai-io/agent/lib/net"
	"github.com/subutai-io/agent/log"
	"github.com/subutai-io/agent/agent/utils"
	"path"
)

type hostStat struct {
	Host string `json:"host"`
	CPU struct {
		Model     string      `json:"model"`
		CoreCount int         `json:"coreCount"`
		Idle      interface{} `json:"idle"`
		Frequency string      `json:"frequency"`
	} `json:"CPU"`
	Disk struct {
		Total interface{} `json:"total"`
		Used  interface{} `json:"used"`
	} `json:"Disk"`
	RAM struct {
		Free   interface{} `json:"free"`
		Total  interface{} `json:"total"`
		Cached interface{} `json:"cached"`
	} `json:"RAM"`
}

type quotaUsage struct {
	Container string `json:"container"`
	CPU       int    `json:"cpu"`
	Disk      int
	RAM       int    `json:"ram"`
}

func queryDB(cmd string) (res []client.Result, err error) {

	clnt, err := utils.InfluxDbClient()

	if err == nil {
		defer clnt.Close()
	} else {
		return nil, err
	}

	q := client.Query{
		Command:  cmd,
		Database: config.Influxdb.Db,
	}
	if response, err := clnt.Query(q); err == nil {
		if response.Error() != nil {
			return res, response.Error()
		}
		res = response.Results
	}
	if len(res) == 0 || len(res[0].Series) == 0 {
		err = errors.New("No result")
	}
	return res, err
}

func ramLoad() (memfree, memtotal, cached interface{}) {
	file, err := os.Open("/proc/meminfo")

	if err == nil {
		defer file.Close()
	}

	if log.Check(log.WarnLevel, "Reading /proc/meminfo", err) {
		return
	}
	scanner := bufio.NewScanner(bufio.NewReader(file))
	for scanner.Scan() {
		line := strings.Fields(strings.Replace(scanner.Text(), ":", "", -1))
		value, _ := strconv.Atoi(line[1])
		if line[0] == "MemTotal" {
			memtotal = value * 1024
		} else if line[0] == "MemFree" {
			memfree = value * 1024
		} else if line[0] == "Cached" {
			cached = value * 1024
		}
	}
	return
}

func getCPUstat() (idle, total uint64) {
	contents, err := ioutil.ReadFile("/proc/stat")
	if err != nil {
		return
	}
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] == "cpu" {
			numFields := len(fields)
			for i := 1; i < numFields; i++ {
				val, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					fmt.Println("Error: ", i, fields[i], err)
				}
				total += val
				if i == 4 {
					idle = val
				}
			}
			return
		}
	}
	return
}

func cpuLoad(h string) interface{} {
	res, err := queryDB("SELECT non_negative_derivative(mean(value),1s) FROM host_cpu WHERE hostname =~ /^" + h + "$/ AND type =~ /idle/ AND time > now() - 1m GROUP BY time(10s), type, hostname fill(none)")
	if err == nil && len(res) > 0 && len(res[0].Series) > 0 && len(res[0].Series[0].Values) > 0 && len(res[0].Series[0].Values[0]) > 1 {
		return res[0].Series[0].Values[0][1]
	}
	idle0, total0 := getCPUstat()
	time.Sleep(3 * time.Second)
	idle1, total1 := getCPUstat()
	cpuUsage := 100 * (float64(total1-total0) - float64(idle1-idle0)) / float64(total1-total0)
	return 100 - cpuUsage
}

func diskLoad() (diskavail, diskused int) {
	out, err := exc.Execute("zfs", "list", path.Join(config.Agent.Dataset))
	log.Check(log.ErrorLevel, "Gettings zfs list "+out, err)

	line := strings.Split(out, "\n")

	if len(line) > 1 {
		fields := strings.Fields(line[1])

		if len(fields) > 2 {
			diskused, _ = fs.ConvertToBytes(fields[1])
			diskavail, _ = fs.ConvertToBytes(fields[2])
		}
	}

	return
}

func cpuQuotaUsage(h string) int {
	cpuCurLoad, err := queryDB("SELECT non_negative_derivative(mean(value), 1s) FROM lxc_cpu WHERE time > now() - 1m and hostname =~ /^" + h + "$/ GROUP BY time(10s), type fill(none)")
	if err != nil {
		log.Warn("No data received for container cpu load")
		return 0
	}
	sys, err := cpuCurLoad[0].Series[0].Values[0][1].(json.Number).Float64()
	user, err := cpuCurLoad[0].Series[1].Values[0][1].(json.Number).Float64()
	log.Check(log.FatalLevel, "Decoding cpu load", err)
	cpuUsage := 0
	if container.QuotaCPU(h, "") != 0 {
		cpuUsage = (int(sys+user) * 100) / container.QuotaCPU(h, "")
	}

	return cpuUsage
}

func read(path string) (i int) {
	f, err := os.Open(path)
	log.Check(log.FatalLevel, "Reading "+path, err)
	defer f.Close()
	scanner := bufio.NewScanner(bufio.NewReader(f))
	for scanner.Scan() {
		i, err = strconv.Atoi(scanner.Text())
		log.Check(log.FatalLevel, "Converting string", err)
	}
	return
}

func ramQuotaUsage(h string) int {
	u := read("/sys/fs/cgroup/memory/lxc/" + h + "/memory.usage_in_bytes")
	l := read("/sys/fs/cgroup/memory/lxc/" + h + "/memory.limit_in_bytes")

	ramUsage := 0
	if l != 0 {
		ramUsage = u * 100 / l
	}

	return ramUsage
}

func diskQuotaUsage(path string) int {
	u, err := fs.DatasetDiskUsage(path)
	if err != nil {
		u = 0
	}

	l, err := fs.GetQuota(path)
	if err != nil {
		l = 0
	}

	diskUsage := 0
	if l != 0 {
		diskUsage = u * 100 / l
	}

	return diskUsage
}

// quota returns Json string with container's resource quota information
func quota(h string) string {
	usage := new(quotaUsage)
	usage.Container = h
	usage.CPU = cpuQuotaUsage(h)
	usage.RAM = ramQuotaUsage(h)
	usage.Disk = diskQuotaUsage(h)

	a, err := json.Marshal(usage)
	if err != nil {
		log.Warn("Cannot marshal sysload result json")
		return ""
	}

	return string(a)
}

// sysload gathers cpu model information with cpu, ram and disk load and returns it as Json string
func sysLoad(h string) string {
	result := new(hostStat)
	result.Host = h
	result.CPU.Idle = cpuLoad(h)
	result.CPU.Model = grep("model name", "/proc/cpuinfo")
	result.CPU.CoreCount = runtime.NumCPU()
	result.CPU.Frequency = grep("cpu MHz", "/proc/cpuinfo")
	result.RAM.Free, result.RAM.Total, result.RAM.Cached = ramLoad()
	diskAvail, diskUsed := diskLoad()
	result.Disk.Total = diskUsed + diskAvail
	result.Disk.Used = diskUsed

	a, err := json.Marshal(result)
	if err != nil {
		log.Warn("Cannot marshal sysload result json")
		return ""
	}

	return string(a)
}

// grep searches for "src" regexp key in "filename" and returns value if found
func grep(str, filename string) string {
	regex, err := regexp.Compile(str)
	if err != nil {
		log.Warn("Cannot compile regexp for: " + str)
		return ""
	}
	fh, err := os.Open(filename)
	if err != nil {
		log.Warn("Cannot open " + filename)
		return ""
	}
	defer fh.Close()

	f := bufio.NewReader(fh)
	for {
		buf, _, err := f.ReadLine()
		if err != nil {
			log.Warn("Cannot read line from file")
			return ""
		}
		if regex.MatchString(string(buf)) {
			line := strings.Split(string(buf), ":")
			return line[1]
		}
	}
}

// Info command's purposed is to display common system information, such as
// external IP address to access the container host quotas, its CPU model, RAM size, etc. It's mainly used for internal SS needs.
func Info(command, host string) {
	if command == "ipaddr" {
		fmt.Println(net.GetIp())
		return
	} else if command == "ports" {
		for k := range usedPorts() {
			fmt.Println(k)
		}
	} else if command == "os" {

		fmt.Printf("%s\n", getOsName())
	} else if command == "id" {
		os.Setenv("GNUPGHOME", config.Agent.GpgHome)
		defer os.Unsetenv("GNUPGHOME")
		fmt.Printf("%s\n", gpg.GetFingerprint("rh@subutai.io"))
	} else if command == "du" {
		usage, err := fs.DatasetDiskUsage(host)
		log.Check(log.ErrorLevel, "Checking disk usage", err)
		fmt.Println(usage)
	} else if command == "quota" {
		if len(host) == 0 {
			log.Error("Usage: subutai info <quota|system> <hostname>")
		}
		fmt.Println(quota(host))
	} else if command == "system" {
		host, err := os.Hostname()
		log.Check(log.DebugLevel, "Getting hostname of the system", err)
		fmt.Println(sysLoad(host))
	}
}

func getOsName() string {

	out, err := exec.Command("/bin/bash", "-c", "cat /etc/*release").Output()

	log.Check(log.ErrorLevel, "Determining OS name", err)

	output := strings.Split(string(out), "\n")

	var version, version2 string

	for _, line := range output {

		if strings.HasPrefix(line, "DISTRIB_DESCRIPTION") {

			version = strings.Trim(strings.Replace(line, "DISTRIB_DESCRIPTION=", "", 1), "\"")

			break
		}

		if strings.HasPrefix(line, "PRETTY_NAME") {

			version2 = strings.Trim(strings.Replace(line, "PRETTY_NAME=", "", 1), "\"")
		}

	}

	if version != "" {
		return version
	} else {
		return version2
	}
}

func usedPorts() map[string]bool {
	ports := make(map[string]bool)

	out, _ := exec.Command("ss", "-ltun").Output()
	scanner := bufio.NewScanner(bytes.NewReader(out))

	for scanner.Scan() {
		line := strings.Fields(scanner.Text())
		if len(line) > 4 && (strings.HasPrefix(line[0], "tcp") || strings.HasPrefix(line[0], "udp")) {
			if socket := strings.Split(line[4], ":"); len(socket) > 1 && socket[0] != "127.0.0.1" {
				ports[strings.TrimSuffix(line[0], "6")+":"+socket[len(socket)-1]] = true
			}
		}
	}
	return ports
}
