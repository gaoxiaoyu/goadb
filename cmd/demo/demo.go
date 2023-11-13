// An app demonstrating most of the library's features.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	adb "github.com/zach-klippenstein/goadb"
	"github.com/zach-klippenstein/goadb/errors"
)

var (
	port = flag.Int("p", adb.AdbPort, "")

	client *adb.Adb
)

func main() {
	flag.Parse()

	var err error
	client, err = adb.NewWithConfig(adb.ServerConfig{
		Port: *port,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Starting server…")
	client.StartServer()

	serverVersion, err := client.ServerVersion()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Server version:", serverVersion)

	devices, err := client.ListDevices()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Devices:")
	for _, device := range devices {
		fmt.Printf("\t%+v\n", *device)
	}

	fmt.Printf("any device: %+v", adb.AnyDevice())

	// PrintDeviceInfoAndError(adb.AnyDevice())
	// PrintDeviceInfoAndError(adb.AnyLocalDevice())
	// PrintDeviceInfoAndError(adb.AnyUsbDevice())

	serials, err := client.ListDeviceSerials()
	if err != nil {
		log.Fatal(err)
	}
	for _, serial := range serials {
		fmt.Println("print device info, serial:", serial)
		PrintDeviceInfoAndError(adb.DeviceWithSerial(serial))

		device := client.Device(adb.DeviceWithSerial(serial))

		//cmd := `"if pgrep -f "com.Fancy.FancyModelEditor" > /dev/null; then echo "Process com.Fancy.FancyModelEditor is already running. Exiting..."; else echo "Process com.Fancy.FancyModelEditor not found. Starting the process..."; am start -n com.Fancy.FancyModelEditor/com.unity3d.player.UnityPlayerActivity; fi"`
		//cmd := "a=\"test\"; if [[ $a == \"test\" ]]; then echo \"matching\"; fi"
		cmd := "if pgrep -f \"com.Fancy.FancyModelEditor\" > /dev/null; then echo \"Process com.Fancy.FancyModelEditor is already running. Exiting...\"; else echo \"Process com.Fancy.FancyModelEditor not found. Starting the process...\"; am start -n com.Fancy.FancyModelEditor/com.unity3d.player.UnityPlayerActivity; fi;"

		if result, err := device.RunCommand(cmd); err != nil {
			fmt.Printf("RunCommand cmd err, result %s, err %s err chain %s\n", result, err.Error(), adb.ErrorWithCauseChain(err))
		} else {
			fmt.Printf("RunCommand  sucess %s\n", result)
		}

		fmt.Println("PushFile start", serial)

		if err := device.PushFile("/home/ericgao/github-1-83-1.apk", "/data/local/tmp/github-1-83-1.apk"); err != nil {
			fmt.Println("PushFile", err.Error())
		}
		if result, err := device.RunCommand("ls", "-al", "/data/local/tmp/github-1-83-1.apk"); err != nil {
			fmt.Printf("RunCommand ls err, result %s, err %s\n", result, err.Error())
		} else {
			fmt.Printf("RunCommand ls sucess %s\n", result)
		}
		// var batchCmds []string
		// cmd := fmt.Sprintf("%s\n%s\n%s", "ls -al /data/local/tmp", "cat /data/local/tmp/tc.sh", "cp /data/local/tmp/zxtask.log zxtask.log1")
		// batchCmds = append(batchCmds, cmd)
		// //batchCmds = append(batchCmds, "ls -al /data/local/tmp; cat /data/local/tmp/tc.sh; cp zxtask.log zxtask.log1;")
		// batchCmds = append(batchCmds, "cat /storage/emulated/0/Android/data/com.Fancy.FancyModelEditor/files/App-CloudClient/Log/startState.log")
		// batchCmds = append(batchCmds, "am start -n com.Fancy.FancyModelEditor/com.unity3d.player.UnityPlayerActivity")

		// for _, cmd := range batchCmds {
		// 	if result, err := device.RunCommand(cmd); err != nil {
		// 		fmt.Printf("RunCommand, batch cmd %s error, err %s\n", cmd, err.Error())
		// 	} else {
		// 		fmt.Printf("RunCommand, batch cmd %s sucess %s\n", cmd, result)
		// 	}
		// }

		time.Sleep(time.Second * 10)
		if result, err := device.RunCommand("ls", "-al", "/data/local/tmp/github-1-83-1.apk"); err != nil {
			fmt.Printf("RunCommand ls err, result %s, err %s\n", result, err.Error())
		} else {
			fmt.Printf("RunCommand ls sucess %s\n", result)
		}

		if result, err := device.RunCommand("pm", "install", "-r", "-d", "-g", "/data/local/tmp/github-1-83-1.apk"); err != nil {
			fmt.Printf("RunCommand err, result %s, err %s\n", result, err.Error())
		} else {
			fmt.Printf("RunCommand sucess %s\n", result)
		}

		// if result, err := device.InstallApk("/data/local/tmp/github-1-83-1.apk"); err != nil {
		// 	fmt.Println("InstallApk err", result, err.Error())
		// } else {
		// 	fmt.Println("InstallApk sucess", result)
		// }

		// if result, err := device.RemoveFile("/data/local/tmp/github-1-83-1.apk"); err != nil {
		// 	fmt.Println("RemoveFile err", result, err.Error())
		// } else {
		// 	fmt.Println("RemoveFile sucess", result)
		// }

	}

	fmt.Println()
	fmt.Println("Watching for device state changes.")
	watcher := client.NewDeviceWatcher()
	for event := range watcher.C() {
		fmt.Printf("\t[%s]%+v\n", time.Now(), event)
	}
	if watcher.Err() != nil {
		printErr(watcher.Err())
	}

	//fmt.Println("Killing server…")
	//client.KillServer()
}

func printErr(err error) {
	switch err := err.(type) {
	case *errors.Err:
		fmt.Println(err.Error())
		if err.Cause != nil {
			fmt.Print("caused by ")
			printErr(err.Cause)
		}
	default:
		fmt.Println("error:", err)
	}
}

func PrintDeviceInfoAndError(descriptor adb.DeviceDescriptor) {
	device := client.Device(descriptor)
	if err := PrintDeviceInfo(device); err != nil {
		log.Println(err)
	}
}

func PrintDeviceInfo(device *adb.Device) error {
	serialNo, err := device.Serial()
	if err != nil {
		return err
	}
	devPath, err := device.DevicePath()
	if err != nil {
		return err
	}
	state, err := device.State()
	if err != nil {
		return err
	}

	fmt.Println(device)
	fmt.Printf("\tserial no: %s\n", serialNo)
	fmt.Printf("\tdevPath: %s\n", devPath)
	fmt.Printf("\tstate: %s\n", state)
	fmt.Printf("\tdescriptor: %s\n", device.String())

	cmdOutput, err := device.RunCommand("pwd")
	if err != nil {
		fmt.Println("\terror running command:", err)
	}
	fmt.Printf("\tcmd output: %s\n", cmdOutput)

	cmdOutput, err = device.RunCommand("cat", "/sys/class/net/eth0/address")
	if err != nil {
		fmt.Println("\terror running command:", err)
	}
	fmt.Printf("\tcmd output: %s\n", cmdOutput)

	// stat, err := device.Stat("/sdcard")
	// if err != nil {
	// 	fmt.Println("\terror stating /sdcard:", err)
	// }
	// fmt.Printf("\tstat \"/sdcard\": %+v\n", stat)

	// fmt.Println("\tfiles in \"/\":")
	// entries, err := device.ListDirEntries("/")
	// if err != nil {
	// 	fmt.Println("\terror listing files:", err)
	// } else {
	// 	for entries.Next() {
	// 		fmt.Printf("\t%+v\n", *entries.Entry())
	// 	}
	// 	if entries.Err() != nil {
	// 		fmt.Println("\terror listing files:", err)
	// 	}
	// }

	// fmt.Println("\tnon-existent file:")
	// stat, err = device.Stat("/supercalifragilisticexpialidocious")
	// if err != nil {
	// 	fmt.Println("\terror:", err)
	// } else {
	// 	fmt.Printf("\tstat: %+v\n", stat)
	// }

	// fmt.Print("\tload avg: ")
	// loadavgReader, err := device.OpenRead("/proc/loadavg")
	// if err != nil {
	// 	fmt.Println("\terror opening file:", err)
	// } else {
	// 	loadAvg, err := ioutil.ReadAll(loadavgReader)
	// 	if err != nil {
	// 		fmt.Println("\terror reading file:", err)
	// 	} else {
	// 		fmt.Println(string(loadAvg))
	// 	}
	// }

	return nil
}
