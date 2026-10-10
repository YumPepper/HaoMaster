package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// ──────────────────────────────────────────────────────────
// 郝大师 · 硬件检测工具  v1.0
// 通过 PowerShell 的 Get-CimInstance 获取硬件信息，输出全中文报告。
// 仅在 Windows 上运行；可在 Linux 上交叉编译生成 .exe。
// ──────────────────────────────────────────────────────────

// ANSI 颜色（Windows 10/11 终端已原生支持）
const (
	cReset = "\033[0m"
	cBold  = "\033[1m"
	cDim   = "\033[2m"
	cCyan  = "\033[36m"
	cGreen = "\033[32m"
	cYel   = "\033[33m"
	cBlue  = "\033[34m"
	cMag   = "\033[35m"
	cRed   = "\033[31m"
)

var report strings.Builder // 纯文本报告（不含颜色码），用于保存到文件

// runPS 执行一条 PowerShell 命令，要求输出为 JSON，返回解析后的对象数组。
func runPS(query string) ([]map[string]interface{}, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", query+" | ConvertTo-Json -Compress -Depth 4")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "null" {
		return nil, nil
	}
	// 先尝试数组，再尝试单个对象
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		return []map[string]interface{}{obj}, nil
	}
	return nil, fmt.Errorf("无法解析 PowerShell 输出")
}

// ─────────── 显示宽度与对齐 ───────────

func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		if r > 0x2E7F { // 简单判定：CJK 等宽字符按 2 列
			w += 2
		} else {
			w += 1
		}
	}
	return w
}

func padRight(s string, width int) string {
	if d := width - dispWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// ─────────── 字段取值 ───────────

func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return "未知"
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "未知"
		}
		return s
	case float64:
		return fmt.Sprintf("%.0f", t)
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getInt64(m map[string]interface{}, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return -1
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		var n int64
		fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return -1
	}
}

// bytes 转 GB 字符串
func toGB(b int64) string {
	if b < 0 {
		return "未知"
	}
	return fmt.Sprintf("%.2f GB", float64(b)/1e9)
}

func toGBInt(b float64) string {
	return fmt.Sprintf("%.2f GB", b/1e9)
}

// 解析 CIM 日期时间，如 20251009120000.000000+480
func parseCIMDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 14 {
		return s
	}
	t, err := time.Parse("20060102150405", s[:14])
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04:05")
}

// ─────────── 输出辅助 ───────────

func printSection(title string) {
	bar := strings.Repeat("─", 56)
	fmt.Printf("\n%s▌ %s%s\n", cCyan+cBold, title, cReset)
	fmt.Printf("%s%s%s\n", cCyan, bar, cReset)
	report.WriteString("\n" + title + "\n" + strings.Repeat("=", 56) + "\n")
}

func printKV(key, val string) {
	fmt.Printf("  %s%s%s: %s\n", cYel, padRight(key, 14), cReset, val)
	report.WriteString("  " + padRight(key, 14) + ": " + val + "\n")
}

func printNote(msg string) {
	fmt.Printf("  %s%s%s\n", cDim, msg, cReset)
	report.WriteString("  " + msg + "\n")
}

// ─────────── 各检测模块 ───────────

func sectionSystem() {
	printSection("系统信息")
	os, _ := runPS("Get-CimInstance Win32_OperatingSystem")
	if len(os) > 0 {
		o := os[0]
		printKV("操作系统", getString(o, "Caption"))
		printKV("版本号", getString(o, "Version")+"  (内部版本 "+getString(o, "BuildNumber")+")")
		printKV("系统架构", getString(o, "OSArchitecture"))
		printKV("系统目录", getString(o, "SystemDirectory"))
		printKV("安装日期", parseCIMDate(getString(o, "InstallDate")))
		printKV("上次启动", parseCIMDate(getString(o, "LastBootUpTime")))
		// 内存
		vis := getInt64(o, "TotalVisibleMemorySize") // KB
		free := getInt64(o, "FreePhysicalMemory")    // KB
		if vis > 0 {
			printKV("物理内存总量", fmt.Sprintf("%.2f GB", float64(vis)/1024/1024))
			if free > 0 {
				printKV("可用内存", fmt.Sprintf("%.2f GB", float64(free)/1024/1024))
			}
		}
	}
	cs, _ := runPS("Get-CimInstance Win32_ComputerSystem")
	if len(cs) > 0 {
		c := cs[0]
		printKV("计算机制造商", getString(c, "Manufacturer"))
		printKV("型号", getString(c, "Model"))
		printKV("计算机名", getString(c, "Name"))
		printKV("当前用户", getString(c, "UserName"))
		printKV("系统类型", getString(c, "SystemType"))
	}
}

func sectionBIOSBoard() {
	printSection("主板与 BIOS 信息")
	bb, _ := runPS("Get-CimInstance Win32_BaseBoard")
	if len(bb) > 0 {
		b := bb[0]
		printKV("主板制造商", getString(b, "Manufacturer"))
		printKV("主板型号", getString(b, "Product"))
		printKV("主板版本", getString(b, "Version"))
		printKV("主板序列号", getString(b, "SerialNumber"))
	}
	bios, _ := runPS("Get-CimInstance Win32_BIOS")
	if len(bios) > 0 {
		b := bios[0]
		printKV("BIOS 制造商", getString(b, "Manufacturer"))
		printKV("BIOS 版本", getString(b, "SMBIOSBIOSVersion"))
		printKV("BIOS 发布日期", parseCIMDate(getString(b, "ReleaseDate")))
		printKV("BIOS 序列号", getString(b, "SerialNumber"))
	}
}

func sectionCPU() {
	printSection("CPU 处理器信息")
	cpus, _ := runPS("Get-CimInstance Win32_Processor")
	if len(cpus) == 0 {
		printNote("未检测到处理器信息")
		return
	}
	for i, c := range cpus {
		if len(cpus) > 1 {
			fmt.Printf("\n  %s[ 处理器 %d ]%s\n", cGreen, i+1, cReset)
			report.WriteString(fmt.Sprintf("\n  [ 处理器 %d ]\n", i+1))
		}
		printKV("名称", getString(c, "Name"))
		printKV("制造商", getString(c, "Manufacturer"))
		printKV("核心数", fmt.Sprintf("%d", getInt64(c, "NumberOfCores")))
		printKV("线程数", fmt.Sprintf("%d", getInt64(c, "NumberOfLogicalProcessors")))
		mhz := getInt64(c, "MaxClockSpeed")
		if mhz > 0 {
			printKV("最大频率", fmt.Sprintf("%d MHz (%.2f GHz)", mhz, float64(mhz)/1000))
		}
		printKV("接口类型", getString(c, "SocketDesignation"))
		printKV("二级缓存", fmt.Sprintf("%d KB", getInt64(c, "L2CacheSize")))
		printKV("三级缓存", fmt.Sprintf("%d KB", getInt64(c, "L3CacheSize")))
	}
}

func sectionMemory() {
	printSection("内存条信息")
	mem, _ := runPS("Get-CimInstance Win32_PhysicalMemory")
	if len(mem) == 0 {
		printNote("未检测到物理内存条信息")
		return
	}
	var total int64
	for i, m := range mem {
		cap := getInt64(m, "Capacity")
		total += cap
		fmt.Printf("\n  %s[ 内存条 %d ]%s\n", cGreen, i+1, cReset)
		report.WriteString(fmt.Sprintf("\n  [ 内存条 %d ]\n", i+1))
		if cap >= 0 {
			printKV("容量", toGB(cap))
		}
		printKV("频率", fmt.Sprintf("%s MHz", getString(m, "Speed")))
		printKV("配置频率", fmt.Sprintf("%s MHz", getString(m, "ConfiguredClockSpeed")))
		printKV("制造商", getString(m, "Manufacturer"))
		printKV("型号", getString(m, "PartNumber"))
		printKV("插槽", getString(m, "DeviceLocator"))
		printKV("数据位宽", fmt.Sprintf("%d 位", getInt64(m, "DataWidth")))
	}
	fmt.Printf("\n  %s内存总容量: %s%s\n", cMag+cBold, toGB(total), cReset)
	report.WriteString("\n  内存总容量: " + toGB(total) + "\n")
}

func sectionDisk() {
	printSection("硬盘与存储设备")
	disks, _ := runPS("Get-CimInstance Win32_DiskDrive")
	if len(disks) == 0 {
		printNote("未检测到硬盘信息")
		return
	}
	for i, d := range disks {
		size := getInt64(d, "Size")
		fmt.Printf("\n  %s[ 硬盘 %d ]%s\n", cGreen, i+1, cReset)
		report.WriteString(fmt.Sprintf("\n  [ 硬盘 %d ]\n", i+1))
		printKV("型号", getString(d, "Model"))
		printKV("接口类型", getString(d, "InterfaceType"))
		printKV("介质类型", getString(d, "MediaType"))
		if size >= 0 {
			printKV("容量", toGB(size))
		}
		printKV("序列号", getString(d, "SerialNumber"))
		printKV("分区数", fmt.Sprintf("%d", getInt64(d, "Partitions")))
		printKV("固件版本", getString(d, "FirmwareRevision"))
	}

	// 逻辑分区
	parts, _ := runPS("Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3'")
	if len(parts) > 0 {
		fmt.Printf("\n  %s[ 逻辑分区 ]%s\n", cGreen, cReset)
		report.WriteString("\n  [ 逻辑分区 ]\n")
		for _, p := range parts {
			f := getInt64(p, "FreeSpace")
			t := getInt64(p, "Size")
			used := ""
			if t > 0 {
				used = fmt.Sprintf("（已用 %.1f GB / 共 %s）", float64(t-f)/1e9, toGB(t))
			}
			printKV(getString(p, "DeviceID")+" 盘", fmt.Sprintf("剩余 %s %s", toGB(f), used))
		}
	}
}

func sectionGPU() {
	printSection("显卡与显示设备")
	gpus, _ := runPS("Get-CimInstance Win32_VideoController")
	if len(gpus) == 0 {
		printNote("未检测到显卡信息")
		return
	}
	for i, g := range gpus {
		fmt.Printf("\n  %s[ 显卡 %d ]%s\n", cGreen, i+1, cReset)
		report.WriteString(fmt.Sprintf("\n  [ 显卡 %d ]\n", i+1))
		printKV("名称", getString(g, "Name"))
		printKV("GPU 型号", getString(g, "VideoProcessor"))
		ram := getInt64(g, "AdapterRAM")
		if ram >= 0 {
			printKV("显存", toGB(ram))
		}
		printKV("驱动版本", getString(g, "DriverVersion"))
		printKV("驱动日期", parseCIMDate(getString(g, "DriverDate")))
		hres := getString(g, "CurrentHorizontalResolution")
		vres := getString(g, "CurrentVerticalResolution")
		if hres != "未知" && vres != "未知" {
			printKV("当前分辨率", hres+" x "+vres)
		}
		rr := getString(g, "CurrentRefreshRate")
		if rr != "未知" {
			printKV("刷新率", rr+" Hz")
		}
	}
}

func sectionNetwork() {
	printSection("网络适配器")
	nics, _ := runPS("Get-CimInstance Win32_NetworkAdapter -Filter 'NetEnabled=True' OR 'PhysicalAdapter=True'")
	// 过滤出物理且启用的网卡
	var phy []map[string]interface{}
	for _, n := range nics {
		if getString(n, "NetEnabled") == "是" || getString(n, "PhysicalAdapter") == "是" {
			phy = append(phy, n)
		}
	}
	if len(phy) == 0 {
		phy = nics
	}
	if len(phy) == 0 {
		printNote("未检测到网络适配器")
		return
	}
	for i, n := range phy {
		fmt.Printf("\n  %s[ 网卡 %d ]%s\n", cGreen, i+1, cReset)
		report.WriteString(fmt.Sprintf("\n  [ 网卡 %d ]\n", i+1))
		printKV("名称", getString(n, "Name"))
		printKV("MAC 地址", getString(n, "MACAddress"))
		speed := getInt64(n, "Speed")
		if speed > 0 {
			printKV("连接速度", fmt.Sprintf("%.0f Mbps", float64(speed)/1e6))
		}
		printKV("连接名称", getString(n, "NetConnectionID"))
		printKV("制造商", getString(n, "Manufacturer"))
	}
}

func sectionSound() {
	printSection("声卡与多媒体设备")
	snds, _ := runPS("Get-CimInstance Win32_SoundDevice")
	if len(snds) == 0 {
		printNote("未检测到声卡信息")
		return
	}
	for i, s := range snds {
		fmt.Printf("\n  %s[ 声卡 %d ]%s\n", cGreen, i+1, cReset)
		report.WriteString(fmt.Sprintf("\n  [ 声卡 %d ]\n", i+1))
		printKV("名称", getString(s, "Name"))
		printKV("制造商", getString(s, "Manufacturer"))
		printKV("状态", getString(s, "Status"))
	}
}

func sectionBattery() {
	printSection("电池信息")
	bats, _ := runPS("Get-CimInstance Win32_Battery")
	if len(bats) == 0 {
		printNote("当前设备未配备电池（台式机常见）")
		return
	}
	for _, b := range bats {
		printKV("名称", getString(b, "Name"))
		printKV("剩余电量", getString(b, "EstimatedChargeRemaining")+"%")
		printKV("电池状态", getString(b, "BatteryStatus"))
		printKV("设计容量", fmt.Sprintf("%d mWh", getInt64(b, "DesignCapacity")))
	}
}

func sectionSummary() {
	printSection("检测概览")
	printNote("检测时间: " + time.Now().Format("2006-01-02 15:04:05"))
	printNote("工具版本: 郝大师 v1.0 (HaoMaster)")
	printNote("运行平台: " + runtime.GOOS + "/" + runtime.GOARCH)
}

// ─────────── 启用 Windows 终端 ANSI 颜色 ───────────

func enableVT() {
	if runtime.GOOS != "windows" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetConsoleMode")
	getStd := kernel32.NewProc("GetStdHandle")
	h, _, _ := getStd.Call(uintptr(0xFFFFFFF5)) // STD_OUTPUT_HANDLE = -11
	// ENABLE_PROCESSED_OUTPUT(1)|ENABLE_WRAP_AT_EOL_OUTPUT(2)|ENABLE_VIRTUAL_TERMINAL_PROCESSING(7)
	proc.Call(h, uintptr(7))

	// 同步设置输出编码为 UTF-8，避免中文乱码
	exec.Command("chcp", "65001").Run()
}

// ─────────── 主流程 ───────────

func banner() {
	bar := strings.Repeat("━", 56)
	fmt.Printf("\n%s%s%s\n", cCyan, "┏"+bar+"┓", cReset)
	fmt.Printf("%s┃%-58s%s\n", cCyan, "", cReset)
	fmt.Printf("%s┃%s%-18s%s%s\n", cCyan, " ", "郝  大  师  ·  硬  件  检  测  工  具", padRight("", 18), cReset)
	fmt.Printf("%s┃%s%s%s%s\n", cCyan, " ", padRight("HaoMaster Hardware Detector  v1.0", 40), padRight("", 18), cReset)
	fmt.Printf("%s┃%-58s%s\n", cCyan, "", cReset)
	fmt.Printf("%s┗%s┛%s\n\n", cCyan, bar, cReset)

	report.WriteString("========================================\n")
	report.WriteString("   郝大师 · 硬件检测工具 v1.0\n")
	report.WriteString("   HaoMaster Hardware Detector\n")
	report.WriteString("========================================\n")
}

func main() {
	enableVT()
	banner()

	fmt.Printf("%s正在检测硬件信息，请稍候……%s\n", cDim, cReset)
	t0 := time.Now()

	sectionSummary()
	sectionSystem()
	sectionBIOSBoard()
	sectionCPU()
	sectionMemory()
	sectionDisk()
	sectionGPU()
	sectionNetwork()
	sectionSound()
	sectionBattery()

	elapsed := time.Since(t0)
	fmt.Printf("\n%s┗ 检测完成，用时 %.2f 秒%s\n", cGreen+cBold, elapsed.Seconds(), cReset)
	report.WriteString("\n检测完成，用时 " + fmt.Sprintf("%.2f", elapsed.Seconds()) + " 秒\n")

	// 保存报告文件
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if dir == "" {
		dir = "."
	}
	fname := filepath.Join(dir, "郝大师_硬件报告.txt")
	if err := os.WriteFile(fname, []byte(report.String()), 0644); err == nil {
		fmt.Printf("%s报告已保存至: %s%s\n", cGreen, fname, cReset)
	}

	fmt.Printf("\n%s按回车键退出……%s", cDim, cReset)
	fmt.Scanln()
}
