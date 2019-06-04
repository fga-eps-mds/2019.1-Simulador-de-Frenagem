package main

import (
	//"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/tarm/serial"
)

// This const's represents the base infomations to get and save the
// informations os a test
const (
	BufferSize            = 48
	SimulatorPortEnv      = "SIMULATOR_PORT"
	DefaultPort           = "/dev/ttyACM0"
	BaudRate              = 115200
	FrequencyReading      = 10
	LogFilePath           = "unbrake.log"
	ApplicationFolderName = "UnBrake"
	UpperSpeedLimit       = 150
	InferiorSpeedLimit    = 150
	TimeSleepWater        = 3
	TimeCooldown          = 3
	TemperatureLimit      = 400
	rightSizeOfSplit      = 11
	delayAcelerateToBrake = 2
)

var wg sync.WaitGroup
var stopCollectingData chan bool
var sigs chan os.Signal
var temperatureCh = make(chan [2]int)
var speedCh = make(chan int)
var snub = Snub{state: Acelerate}
var portCh = make(chan *serial.Port, 3)
var stabilizing = false
var throwingWater = false

// This const's represents the possible states of a snub
const (
	CoolDown            = string(iota + '$') //'$'
	Acelerate                                //'%'
	Brake                                    //'&'
	AcelerateBrake                           //'''
	CoolDownWater                            //'('
	AcelerateWater                           //')'
	BrakeWater                               //'*'
	AcelerateBrakeWater                      //'+'
)

var currentToNextState = map[string]string{
	Acelerate:      Brake,
	Brake:          CoolDown,
	CoolDown:       Acelerate,
	AcelerateWater: BrakeWater,
	BrakeWater:     CoolDownWater,
	CoolDownWater:  AcelerateWater,
}

var offToOnWater = map[string]string{
	Acelerate:      AcelerateWater,
	Brake:          BrakeWater,
	CoolDown:       CoolDownWater,
	AcelerateWater: AcelerateWater,
	BrakeWater:     BrakeWater,
	CoolDownWater:  CoolDownWater,
}

var onToOffWater = map[string]string{
	AcelerateWater: Acelerate,
	BrakeWater:     Brake,
	CoolDownWater:  CoolDown,
	Acelerate:      Acelerate,
	Brake:          Brake,
	CoolDown:       CoolDown,
}

var byteToStateName = map[string]string{
	"$":  "CoolDown",
	"%":  "Acelerate",
	"&":  "Brake",
	"\"": "AcelerateBrake",
	"(":  "CoolDownWater",
	")":  "AcelerateWater",
	"*":  "BrakeWater",
	"+":  "AcelerateBrakeWater",
}

// Snub is a cycle of aceleration, brake and cooldown
type Snub struct {
	state string
	mux   sync.Mutex
}

func main() {

	logFile := getLogFile()
	defer logFile.Close()

	log.Println("--------------------------------------------")
	log.Println("Initializing application...")

	sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)

	onExit := func() {
		snub.state = CoolDown
		log.Println("Exiting...")
	}
	systray.Run(onReady, onExit)

	log.Println("Application finished!")
	log.Println("--------------------------------------------")
}

//func getSnub() Snub {
//	snub := Snub{state: Acelerate}
//	return snub
//}

func (snub *Snub) updateState() {

	stabilizing = true
	log.Printf("Stabilizing...\n")
	time.Sleep(time.Second * delayAcelerateToBrake)

	snub.mux.Lock()
	defer snub.mux.Unlock()
	oldState := snub.state
	snub.state = currentToNextState[snub.state]
	log.Printf("Change state: %v ---> %v\n", byteToStateName[oldState], byteToStateName[snub.state])
	stabilizing = false

}

func (snub *Snub) turnOnWater(port *serial.Port) {

	snub.mux.Lock()

	oldState := snub.state
	snub.state = offToOnWater[snub.state]
	throwingWater = true
	_, err := port.Write([]byte(snub.state))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Turn on water(%vs): %v ---> %v\n", TimeSleepWater, byteToStateName[oldState], byteToStateName[snub.state])

	snub.mux.Unlock()

	time.Sleep(time.Second * TimeSleepWater)

	snub.mux.Lock()

	oldState = snub.state
	snub.state = onToOffWater[snub.state]
	_, err = port.Write([]byte(snub.state))
	if err != nil {
		log.Fatal(err)
	}
	throwingWater = false
	log.Printf("Turn off water: %v ---> %v\n", byteToStateName[oldState], byteToStateName[snub.state])

	snub.mux.Unlock()
}

func (snub *Snub) handleBrake(port *serial.Port) {
	snub.mux.Lock()

	oldState := snub.state
	snub.state = currentToNextState[snub.state]
	_, err := port.Write([]byte(snub.state))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Change state: %v ---> %v\n", byteToStateName[oldState], byteToStateName[snub.state])

	snub.mux.Unlock()

	time.Sleep(time.Second * TimeCooldown)

	snub.mux.Lock()

	oldState = snub.state
	snub.state = currentToNextState[snub.state]
	_, err = port.Write([]byte(snub.state))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Change state: %v ---> %v\n", byteToStateName[oldState], byteToStateName[snub.state])

	snub.mux.Unlock()
}

func onReady() {
	systray.SetIcon(icon)
	systray.SetTitle("UnBrake")
	systray.SetTooltip("UnBrake")

	mQuitOrig := systray.AddMenuItem("Sair", "Fechar UnBrake")

	stopCollectingData = make(chan bool, 1)

	// Wait for quitting
	go func() {
		select {
		case <-mQuitOrig.ClickedCh:
			log.Println("Quitting request by interface")
		case <-sigs:
			log.Println("Quitting request by signal")
		}

		stopCollectingData <- true
		systray.Quit()
		log.Println("Finished systray")
	}()

	wg.Add(1)
	go collectData()
	go handleSnubstate()

	wg.Wait()
}

func collectData() {
	defer wg.Done()

	const readingDelay = time.Second / FrequencyReading
	simulatorPort := getSimulatorPort()

	//log.Println("Initializing collectData routine...")
	log.Printf("Simulator Port = %s", simulatorPort)
	log.Printf("Buffer size = %d", BufferSize)
	log.Printf("Baud rate = %d", BaudRate)
	log.Printf("Reading delay = %v", readingDelay)

	c := &serial.Config{
		Name: simulatorPort,
		Baud: BaudRate,
	}

	port, err := serial.OpenPort(c)

	if err != nil {
		log.Fatal(err)
	}

	portCh <- port
	close(portCh)

	port.Flush()
	continueCollecting := true
	for continueCollecting {
		select {
		case stop := <-stopCollectingData:
			if stop {
				_, err := port.Write([]byte(CoolDown))
				if err != nil {
					log.Fatal(err)
				}
				continueCollecting = false
			}
		case sig := <-sigs:
			log.Println("Signal received: ", sig)
			_, err := port.Write([]byte(CoolDown))
			if err != nil {
				log.Fatal(err)
			}
			continueCollecting = false
		default:
			getData(port, "\"")
			time.Sleep(readingDelay)
		}
	}
}

func getData(port *serial.Port, command string) []byte {

	n, err := port.Write([]byte(command))

	if err != nil {
		log.Fatal(err)
	}

	buf := make([]byte, BufferSize)
	n, err = port.Read(buf)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%s\n", strings.TrimSpace(string(buf[:n])))

	split := strings.Split(string(buf[:n]), ",")

	if len(split) == rightSizeOfSplit {

		speed, _ := strconv.Atoi(split[6])

		speedCh <- speed

		firstTemperature, _ := strconv.Atoi(split[1])
		secondTemperature, _ := strconv.Atoi(split[2])

		temperatureCh <- [2]int{firstTemperature, secondTemperature}
	}

	return buf
}

func handleSnubstate() {

	port := <-portCh

	for {

		select {

		case speed := <-speedCh:

			if (snub.state == Acelerate || snub.state == AcelerateWater) && !stabilizing {
				if speed >= UpperSpeedLimit {
					go snub.updateState()
					_, err := port.Write([]byte(snub.state))
					if err != nil {
						log.Fatal(err)
					}
				}
			}

			if snub.state == Brake || snub.state == BrakeWater {
				if speed < InferiorSpeedLimit {
					go snub.handleBrake(port)
				}
			}

		case temperature := <-temperatureCh:

			if (temperature[0] > TemperatureLimit || temperature[1] > TemperatureLimit) && !throwingWater {
				if snub.state == Acelerate || snub.state == Brake || snub.state == CoolDown {
					go snub.turnOnWater(port)
				}
			}

		default:
		}
	}
}

func getLogFile() *os.File {
	logPath := ""
	if runtime.GOOS != "windows" {
		logPath = path.Join("/home", os.Getenv("USER"), ApplicationFolderName, "logs")
	} else {
		log_path = path.Join(os.Getenv("APPDATA"), APPLICATION_FOLDER_NAME, "logs")
	}

	os.MkdirAll(logPath, os.ModePerm)
	logPath = path.Join(logPath, LogFilePath)

	logFile, err := os.OpenFile(logPath, os.O_SYNC|os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(logFile)

	return logFile
}

func getSimulatorPort() string {
	log.Println("Getting simulator port...")

	simulatorPort, doesExists := os.LookupEnv(SimulatorPortEnv)

	if !doesExists {
		err := os.Setenv(SimulatorPortEnv, DefaultPort)
		if err != nil {
			log.Fatal(err)
		}

		simulatorPort = os.Getenv(SimulatorPortEnv)
	}

	log.Println("Got simulator port: ", simulatorPort)
	return simulatorPort
}

var icon = []byte{
	0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x20, 0x20, 0x00, 0x00, 0x01, 0x00,
	0x20, 0x00, 0xa8, 0x10, 0x00, 0x00, 0x16, 0x00, 0x00, 0x00, 0x28, 0x00,
	0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00, 0x01, 0x00,
	0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xec, 0xe7, 0xdd, 0x00, 0xf0, 0xeb,
	0xe1, 0x06, 0xf0, 0xeb, 0xe1, 0x21, 0xf0, 0xeb, 0xe1, 0x5f, 0xf0, 0xeb,
	0xe1, 0x9c, 0xf0, 0xeb, 0xe1, 0xcc, 0xf0, 0xeb, 0xe1, 0xec, 0xee, 0xe9,
	0xdc, 0xfa, 0xe7, 0xe2, 0xce, 0xf7, 0xe6, 0xe1, 0xcd, 0xe1, 0xe6, 0xe1,
	0xcd, 0xba, 0xe6, 0xe1, 0xcd, 0x83, 0xe6, 0xe1, 0xcd, 0x42, 0xe6, 0xe1,
	0xcd, 0x11, 0xe6, 0xe1, 0xcd, 0x02, 0x11, 0x10, 0x0e, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc5, 0xc1, 0xb9, 0x00, 0xf0, 0xeb,
	0xe1, 0x04, 0xf0, 0xeb, 0xe1, 0x2e, 0xf0, 0xeb, 0xe1, 0x8f, 0xf0, 0xeb,
	0xe1, 0xdd, 0xf0, 0xeb, 0xe1, 0xf8, 0xf0, 0xeb, 0xe1, 0xfe, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe9, 0xdc, 0xff, 0xe7, 0xe2,
	0xce, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xfd, 0xe6, 0xe1, 0xcd, 0xf1, 0xe6, 0xe1, 0xcd, 0xc2, 0xe6, 0xe1,
	0xcd, 0x62, 0xe6, 0xe1, 0xcd, 0x13, 0xe5, 0xe0, 0xcd, 0x01, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xee, 0xe9,
	0xdf, 0x00, 0xf0, 0xeb, 0xe1, 0x10, 0xf0, 0xeb, 0xe1, 0x72, 0xf0, 0xeb,
	0xe1, 0xdf, 0xf0, 0xeb, 0xe1, 0xfd, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xee, 0xe9, 0xdc, 0xff, 0xe7, 0xe2, 0xce, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xf6, 0xe6, 0xe1,
	0xcd, 0xb8, 0xe6, 0xe1, 0xcd, 0x3c, 0xe6, 0xe1, 0xce, 0x04, 0x10, 0x10,
	0x0d, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xef, 0xea, 0xe0, 0x00, 0xf0, 0xeb, 0xe1, 0x1d, 0xf0, 0xeb,
	0xe1, 0xa2, 0xf0, 0xeb, 0xe1, 0xf6, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe9,
	0xdc, 0xff, 0xe7, 0xe2, 0xce, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xfe, 0xe6, 0xe1,
	0xcd, 0xe1, 0xe6, 0xe1, 0xcd, 0x5f, 0xe6, 0xe1, 0xcd, 0x07, 0x11, 0x10,
	0x0e, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xee, 0xe9, 0xdf, 0x00, 0xf0, 0xeb,
	0xe1, 0x1d, 0xf0, 0xeb, 0xe1, 0xb2, 0xf0, 0xeb, 0xe1, 0xfb, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe9, 0xdc, 0xff, 0xe7, 0xe2,
	0xce, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xed, 0xe6, 0xe1, 0xcd, 0x68, 0xe6, 0xe1, 0xcd, 0x06, 0x0b, 0x0a,
	0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc5, 0xc1,
	0xb9, 0x00, 0xf0, 0xeb, 0xe1, 0x10, 0xf0, 0xeb, 0xe1, 0xa3, 0xf0, 0xeb,
	0xe1, 0xfb, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xee, 0xe9, 0xdc, 0xff, 0xe7, 0xe2, 0xce, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xea, 0xe6, 0xe1, 0xcd, 0x53, 0xe6, 0xe1, 0xcd, 0x02, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb, 0xe1, 0x04, 0xf0, 0xeb,
	0xe1, 0x73, 0xf0, 0xeb, 0xe1, 0xf7, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe9,
	0xdc, 0xff, 0xe7, 0xe2, 0xce, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xd5, 0xe6, 0xe1, 0xcd, 0x29, 0xe3, 0xde, 0xcc, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xed, 0xe8,
	0xde, 0x00, 0xf0, 0xeb, 0xe1, 0x2f, 0xf0, 0xeb, 0xe1, 0xe0, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xea, 0xe0, 0xff, 0xec, 0xe6,
	0xdb, 0xff, 0xe4, 0xdd, 0xd1, 0xff, 0xd9, 0xd2, 0xc2, 0xff, 0xd3, 0xcd,
	0xb8, 0xff, 0xdd, 0xd7, 0xc3, 0xff, 0xe4, 0xde, 0xca, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xfd, 0xe6, 0xe1,
	0xcd, 0x98, 0xe6, 0xe1, 0xcd, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb, 0xe1, 0x06, 0xf0, 0xeb,
	0xe1, 0x91, 0xf0, 0xeb, 0xe1, 0xfd, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xeb, 0xe5,
	0xda, 0xff, 0xce, 0xc4, 0xb3, 0xff, 0xa5, 0x95, 0x77, 0xff, 0x8c, 0x75,
	0x47, 0xff, 0x80, 0x69, 0x38, 0xff, 0x6e, 0x5d, 0x36, 0xff, 0x7d, 0x6e,
	0x4f, 0xff, 0xa6, 0x9c, 0x85, 0xff, 0xd2, 0xcc, 0xb7, 0xff, 0xe5, 0xe0,
	0xcb, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xea, 0xe6, 0xe1,
	0xcd, 0x39, 0xe4, 0xdf, 0xcb, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xf0, 0xeb, 0xe1, 0x23, 0xf0, 0xeb, 0xe1, 0xdf, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xe7, 0xe0, 0xd5, 0xff, 0xb1, 0xa3, 0x8a, 0xff, 0x84, 0x6a,
	0x2e, 0xff, 0x7d, 0x60, 0x0a, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x75, 0x5a,
	0x08, 0xff, 0x60, 0x49, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5f, 0x4a,
	0x0f, 0xff, 0x79, 0x69, 0x4a, 0xff, 0xc0, 0xb9, 0xa3, 0xff, 0xe4, 0xdf,
	0xcb, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xfd, 0xe6, 0xe1, 0xcd, 0x8e, 0xe6, 0xe1,
	0xcd, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb,
	0xe1, 0x62, 0xf0, 0xeb, 0xe1, 0xf8, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xeb, 0xe5, 0xda, 0xff, 0xb0, 0xa2,
	0x8a, 0xff, 0x80, 0x64, 0x1c, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x75, 0x5a, 0x08, 0xff, 0x60, 0x49,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x08, 0xff, 0x71, 0x60, 0x3d, 0xff, 0xc6, 0xbf, 0xaa, 0xff, 0xe5, 0xe0,
	0xcc, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xd3, 0xe6, 0xe1, 0xcd, 0x16, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb, 0xe1, 0x9f, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xef, 0xea,
	0xe0, 0xff, 0xcd, 0xc3, 0xb2, 0xff, 0x83, 0x69, 0x2e, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x75, 0x5a, 0x08, 0xff, 0x60, 0x49, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x09, 0xff, 0x86, 0x79, 0x5d, 0xff, 0xdc, 0xd6, 0xc2, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xf1, 0xe6, 0xe1, 0xcd, 0x3c, 0x14, 0x13, 0x11, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xf0, 0xeb, 0xe1, 0xd0, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xeb, 0xe6, 0xdb, 0xff, 0xa4, 0x93,
	0x75, 0xff, 0x7d, 0x60, 0x0a, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xfc, 0x76, 0x5a,
	0x08, 0xf0, 0x60, 0x49, 0x07, 0xf5, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x65, 0x51,
	0x23, 0xff, 0xc0, 0xb9, 0xa3, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xf9, 0xe6, 0xe1,
	0xcd, 0x64, 0xe5, 0xe0, 0xcc, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb,
	0xe1, 0xf1, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xe3, 0xdc, 0xcf, 0xff, 0x8c, 0x74, 0x45, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xfc, 0x7d, 0x60, 0x09, 0xa6, 0x77, 0x5b, 0x09, 0x38, 0x5f, 0x49,
	0x07, 0x59, 0x5e, 0x48, 0x07, 0xe0, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x09, 0xff, 0xa5, 0x9b,
	0x84, 0xff, 0xe4, 0xdf, 0xcb, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe6, 0xe1, 0xcd, 0xfd, 0xe6, 0xe1, 0xcd, 0x7e, 0xe6, 0xe1,
	0xce, 0x01, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb, 0xe1, 0xfd, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xdb, 0xd4,
	0xc5, 0xff, 0x86, 0x6d, 0x38, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xef, 0x7d, 0x60,
	0x09, 0x35, 0x7b, 0x5f, 0x09, 0x00, 0x5f, 0x49, 0x07, 0x05, 0x5e, 0x48,
	0x07, 0x94, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x97, 0x8c, 0x73, 0xff, 0xe2, 0xdd,
	0xc9, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe1, 0xdb, 0xcd, 0xff, 0xcc, 0xc4,
	0xca, 0xff, 0xc8, 0xc0, 0xca, 0xff, 0xc8, 0xc0, 0xca, 0xff, 0xc8, 0xc0,
	0xca, 0xff, 0xb8, 0xae, 0xc9, 0xaa, 0x51, 0x25, 0xc3, 0x49, 0x48, 0x00,
	0xc3, 0x2c, 0xf0, 0xeb, 0xe1, 0xfa, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xdd, 0xd6, 0xc8, 0xff, 0x87, 0x6f,
	0x3b, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xf4, 0x7d, 0x60, 0x09, 0x53, 0x79, 0x5d,
	0x09, 0x04, 0x5e, 0x48, 0x07, 0x11, 0x5e, 0x48, 0x07, 0xae, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x9b, 0x91, 0x78, 0xff, 0xe3, 0xde, 0xc9, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xbc, 0xb2, 0xc9, 0xff, 0x5d, 0x3c, 0xc4, 0xff, 0x57, 0x30,
	0xc3, 0xff, 0x57, 0x30, 0xc3, 0xff, 0x57, 0x30, 0xc3, 0xff, 0x51, 0x23,
	0xc3, 0xfa, 0x48, 0x01, 0xc3, 0xf4, 0x48, 0x00, 0xc3, 0xdf, 0xf0, 0xeb,
	0xe1, 0xe5, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xe7, 0xe1, 0xd5, 0xff, 0x92, 0x7d, 0x55, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xfe, 0x7d, 0x60, 0x09, 0xdc, 0x76, 0x5b, 0x08, 0x8f, 0x5f, 0x49,
	0x07, 0xab, 0x5e, 0x48, 0x07, 0xf6, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x60, 0x4a, 0x10, 0xff, 0xaf, 0xa6,
	0x8f, 0xff, 0xe5, 0xe0, 0xcc, 0xff, 0xe5, 0xdf, 0xcd, 0xff, 0xa5, 0x98,
	0xc7, 0xff, 0x49, 0x04, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xf0, 0xf0, 0xeb, 0xe1, 0xbe, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe8,
	0xde, 0xff, 0xb4, 0xa7, 0x8f, 0xff, 0x7e, 0x61, 0x11, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x75, 0x5a, 0x08, 0xfe, 0x60, 0x49, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x6d, 0x5b, 0x36, 0xff, 0xcd, 0xc6, 0xb1, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe0, 0xdb, 0xcd, 0xff, 0x87, 0x74, 0xc6, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xce, 0xf0, 0xeb, 0xe1, 0x87, 0xf0, 0xeb, 0xe1, 0xfd, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xdd, 0xd5,
	0xc8, 0xff, 0x8f, 0x79, 0x4e, 0xff, 0x7d, 0x60, 0x0a, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x75, 0x5a,
	0x08, 0xff, 0x60, 0x49, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x60, 0x4b, 0x13, 0xff, 0xa1, 0x97,
	0x7f, 0xff, 0xe2, 0xdd, 0xc9, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xcf, 0xc7,
	0xcb, 0xff, 0x5f, 0x3e, 0xc4, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0x9e, 0xf0, 0xeb,
	0xe1, 0x46, 0xf0, 0xeb, 0xe1, 0xf2, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xef, 0xe9, 0xdf, 0xff, 0xcc, 0xc2,
	0xb1, 0xff, 0x8a, 0x71, 0x40, 0xff, 0x7d, 0x60, 0x0b, 0xff, 0x7d, 0x60,
	0x09, 0xff, 0x7d, 0x60, 0x09, 0xff, 0x75, 0x5a, 0x08, 0xff, 0x60, 0x49,
	0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x5e, 0x48, 0x07, 0xff, 0x60, 0x4b,
	0x13, 0xff, 0x8e, 0x82, 0x68, 0xff, 0xd8, 0xd2, 0xbe, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe3, 0xdd, 0xcd, 0xff, 0xa0, 0x92, 0xc7, 0xff, 0x4b, 0x0e,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xf9, 0x48, 0x00, 0xc3, 0x63, 0xf0, 0xeb, 0xe1, 0x12, 0xf0, 0xeb,
	0xe1, 0xc5, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xee, 0xe9, 0xde, 0xff, 0xd1, 0xc8,
	0xb8, 0xff, 0x9a, 0x87, 0x64, 0xff, 0x81, 0x66, 0x25, 0xff, 0x7d, 0x60,
	0x0b, 0xff, 0x75, 0x5a, 0x08, 0xff, 0x60, 0x49, 0x07, 0xff, 0x5f, 0x4a,
	0x0f, 0xff, 0x6c, 0x5b, 0x35, 0xff, 0xa0, 0x96, 0x7e, 0xff, 0xd8, 0xd2,
	0xbe, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe5, 0xe0, 0xcd, 0xff, 0xc4, 0xbc,
	0xca, 0xff, 0x5e, 0x3d, 0xc4, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xe4, 0x48, 0x00,
	0xc3, 0x27, 0xf0, 0xeb, 0xe1, 0x02, 0xf0, 0xeb, 0xe1, 0x67, 0xf0, 0xeb,
	0xe1, 0xf7, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xef, 0xea, 0xe0, 0xff, 0xe6, 0xe0,
	0xd4, 0xff, 0xcc, 0xc2, 0xb1, 0xff, 0xb4, 0xa6, 0x8f, 0xff, 0xa3, 0x95,
	0x7a, 0xff, 0x9a, 0x8f, 0x76, 0xff, 0xad, 0xa5, 0x8e, 0xff, 0xcc, 0xc6,
	0xb0, 0xff, 0xe2, 0xdd, 0xc8, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe5, 0xe0,
	0xcd, 0xff, 0xce, 0xc6, 0xcb, 0xff, 0x74, 0x5c, 0xc5, 0xff, 0x49, 0x03,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xfe, 0x48, 0x00, 0xc3, 0xa1, 0x48, 0x00, 0xc3, 0x08, 0x12, 0x11,
	0x10, 0x00, 0xf0, 0xeb, 0xe1, 0x15, 0xf0, 0xeb, 0xe1, 0xbd, 0xf0, 0xeb,
	0xe1, 0xfe, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xee, 0xe9, 0xde, 0xff, 0xea, 0xe4, 0xd7, 0xff, 0xe3, 0xde,
	0xca, 0xff, 0xe5, 0xe0, 0xcc, 0xff, 0xe6, 0xe1, 0xcd, 0xff, 0xe6, 0xe1,
	0xcd, 0xff, 0xe3, 0xdd, 0xcd, 0xff, 0xc4, 0xbb, 0xca, 0xff, 0x73, 0x5c,
	0xc5, 0xff, 0x49, 0x06, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xee, 0x48, 0x00,
	0xc3, 0x44, 0x48, 0x00, 0xc2, 0x00, 0x00, 0x00, 0x00, 0x00, 0xef, 0xeb,
	0xe1, 0x01, 0xf0, 0xeb, 0xe1, 0x40, 0xf0, 0xeb, 0xe1, 0xe3, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xee, 0xe9, 0xdc, 0xff, 0xe6, 0xe1, 0xce, 0xff, 0xe4, 0xdf,
	0xcd, 0xff, 0xe0, 0xdb, 0xcc, 0xff, 0xce, 0xc7, 0xcb, 0xff, 0x9e, 0x91,
	0xc7, 0xff, 0x5d, 0x3c, 0xc4, 0xff, 0x48, 0x03, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xfd, 0x48, 0x00, 0xc3, 0x9f, 0x48, 0x00, 0xc3, 0x0a, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0xeb,
	0xe1, 0x04, 0xf0, 0xeb, 0xe1, 0x64, 0xf0, 0xeb, 0xe1, 0xee, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xe9, 0xe3,
	0xdb, 0xff, 0xbb, 0xb1, 0xca, 0xff, 0xa4, 0x97, 0xc7, 0xff, 0x85, 0x72,
	0xc6, 0xff, 0x5e, 0x3d, 0xc4, 0xff, 0x4a, 0x0e, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xd8, 0x48, 0x00,
	0xc3, 0x2c, 0x47, 0x00, 0xc1, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x12, 0x11, 0x10, 0x00, 0xf0, 0xeb,
	0xe1, 0x08, 0xf0, 0xeb, 0xe1, 0x6d, 0xf0, 0xeb, 0xe1, 0xec, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xd5, 0xcd, 0xda, 0xff, 0x5f, 0x3e,
	0xc5, 0xff, 0x49, 0x03, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xed, 0x48, 0x00, 0xc3, 0x58, 0x48, 0x00, 0xc3, 0x02, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc4, 0xc0, 0xb8, 0x00, 0xf0, 0xeb,
	0xe1, 0x06, 0xf0, 0xeb, 0xe1, 0x57, 0xf0, 0xeb, 0xe1, 0xd8, 0xf0, 0xeb,
	0xe1, 0xfd, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xd2, 0xca, 0xd9, 0xff, 0x59, 0x34, 0xc4, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xf1, 0x48, 0x00, 0xc3, 0x72, 0x48, 0x00,
	0xc3, 0x07, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x0f, 0x0e, 0x00, 0xf0, 0xeb,
	0xe1, 0x02, 0xf0, 0xeb, 0xe1, 0x2c, 0xf0, 0xeb, 0xe1, 0x9d, 0xf0, 0xeb,
	0xe1, 0xec, 0xf0, 0xeb, 0xe1, 0xfd, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb,
	0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xf0, 0xeb, 0xe1, 0xff, 0xd2, 0xca,
	0xd9, 0xff, 0x59, 0x34, 0xc4, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xec, 0x48, 0x00,
	0xc3, 0x72, 0x48, 0x00, 0xc3, 0x09, 0x3a, 0x00, 0xa0, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xee, 0xe9,
	0xdf, 0x00, 0xf0, 0xeb, 0xe1, 0x09, 0xf0, 0xeb, 0xe1, 0x3c, 0xf0, 0xeb,
	0xe1, 0x92, 0xf0, 0xeb, 0xe1, 0xd6, 0xf0, 0xeb, 0xe1, 0xf2, 0xf0, 0xeb,
	0xe1, 0xfa, 0xf0, 0xeb, 0xe1, 0xfe, 0xd2, 0xca, 0xd9, 0xff, 0x59, 0x34,
	0xc4, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xfd, 0x48, 0x00,
	0xc3, 0xd7, 0x48, 0x00, 0xc3, 0x57, 0x48, 0x00, 0xc3, 0x07, 0x3a, 0x00,
	0xa0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0xed, 0xe8, 0xde, 0x00, 0xf0, 0xeb, 0xe1, 0x05, 0xf0, 0xeb,
	0xe1, 0x18, 0xf0, 0xeb, 0xe1, 0x40, 0xf0, 0xeb, 0xe1, 0x68, 0xf0, 0xeb,
	0xe1, 0x82, 0xc2, 0xb9, 0xd6, 0xab, 0x52, 0x27, 0xc4, 0xfa, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00, 0xc3, 0xfe, 0x48, 0x00,
	0xc3, 0xed, 0x48, 0x00, 0xc3, 0x9e, 0x48, 0x00, 0xc3, 0x2c, 0x48, 0x00,
	0xc3, 0x02, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xc5, 0xc1,
	0xb9, 0x00, 0xef, 0xea, 0xe0, 0x00, 0xf0, 0xeb, 0xe1, 0x02, 0x55, 0x2c,
	0xc4, 0x45, 0x48, 0x02, 0xc3, 0xf3, 0x48, 0x00, 0xc3, 0xff, 0x48, 0x00,
	0xc3, 0xff, 0x48, 0x00, 0xc3, 0xfe, 0x48, 0x00, 0xc3, 0xf8, 0x48, 0x00,
	0xc3, 0xe3, 0x48, 0x00, 0xc3, 0x9f, 0x48, 0x00, 0xc3, 0x42, 0x48, 0x00,
	0xc3, 0x0a, 0x47, 0x00, 0xc1, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x48, 0x00, 0xc3, 0x28, 0x48, 0x00,
	0xc3, 0xda, 0x48, 0x00, 0xc3, 0xec, 0x48, 0x00, 0xc3, 0xca, 0x48, 0x00,
	0xc3, 0x9a, 0x48, 0x00, 0xc3, 0x61, 0x48, 0x00, 0xc3, 0x26, 0x48, 0x00,
	0xc3, 0x08, 0x48, 0x00, 0xc2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x00, 0x03, 0xff, 0xfc, 0x00,
	0x00, 0xff, 0xf8, 0x00, 0x00, 0x7f, 0xf0, 0x00, 0x00, 0x3f, 0xe0, 0x00,
	0x00, 0x1f, 0xc0, 0x00, 0x00, 0x0f, 0x80, 0x00, 0x00, 0x0f, 0x80, 0x00,
	0x00, 0x07, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00,
	0x00, 0x03, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00,
	0x00, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x80, 0x00,
	0x00, 0x01, 0x80, 0x00, 0x00, 0x01, 0xc0, 0x00, 0x00, 0x03, 0xe0, 0x00,
	0x00, 0x03, 0xf0, 0x00, 0x00, 0x07, 0xf8, 0x00, 0x00, 0x0f, 0xfe, 0x00,
	0x00, 0x1f, 0xff, 0x80, 0x00, 0x3f, 0xff, 0xf8, 0x00, 0xff, 0xff, 0xfc,
	0x03, 0xff,
}
