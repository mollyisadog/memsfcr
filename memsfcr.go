package main

import (
	"andrewj.com/readmems/rosco"
	"encoding/json"
	"flag"
	"fmt"
	"go.bug.st/serial.v1"
	"strconv"
	"time"
)

const version = "v0.1.0"

var header = fmt.Sprintf("\nMemsFCR %s\n", version)
var config *rosco.ReadmemsConfig
var paused = false
var logging = false

func helpMessage() string {
	return fmt.Sprintf(`%s
	ROSCO MEMS 1.6 Fault Code Reader

    Usage:
	memsfcr [flags]
	  
    Flags:
	-port		Name/path of the serial port 
	-command	Command to execute on the ECU {read}
	-loop		Command execution loop count, use 'inf' for infinite
	-output		Use 'stdout' to send response to console, 'file' to log in CSV format to a file {stdout | file}
	-wait		Retry the connection until a connection is established {true | false}
	-help		This help message
	`, header)
}

func memsCommandResponseLoop(config *rosco.ReadmemsConfig) {
	const DataInterval = 500 * time.Millisecond
	const HeartbeatInterval = 2000 * time.Millisecond
	var logger *rosco.MemsDataLogger

	if config.Output == "file" {
		logging = true
		logger = rosco.NewMemsDataLogger()
	}

	// connect and initialise the ECU
	mems := rosco.NewMemsConnection()
	mems.ConnectAndInitialiseECU(config)

	for {
		// wait for comms

		if mems.SerialPort == nil {
			// exit if the serial port is disconnected
			rosco.LogI.Println("Lost connection to ECU, exiting")
			// break
		}

		if mems.Exit == true {
			// exit if the serial port is disconnected
			rosco.LogI.Println("Exit requested, exiting")
			break
		}

		count, _ := strconv.Atoi(config.Loop)

		// start a loop for listening for events from the web interface
		go recieveMessageFromWebViewLoop(mems)

		// enter a command / response loop
		for loop := 0; loop < count; {
			if paused {
				// send a periodic heartbeat to keep the connection alive when paused
				rosco.LogI.Printf("sending heatbeat")
				go sendCommandToMemsChannel(mems, rosco.MEMS_Heartbeat)

				// send heatbeats at a slower interval to data frame requests
				time.Sleep(HeartbeatInterval)

			} else {
				// read data from the ECU
				rosco.LogI.Printf("sending dataframe request to ECU")
				mems.ReadMemsData()

				// wait for response
				rosco.LogI.Printf("waiting for response from ECU")
				data := <-mems.ReceivedFromECU
				rosco.LogI.Printf("received dataframe from ECU")

				// send it to the web interface
				sendDataToWebView(data.MemsDataFrame)

				if logging {
					// write to a log file if logging is enabled
					go logger.WriteMemsDataToFile(data.MemsDataFrame)
				}

				// increment count of data calls
				// don't increment if we're paused
				loop = loop + 1

				// sleep between calls to give the ECU time to catch up
				// the ECU will get slower as load increases so this ensures
				// a regular time series for the data set
				time.Sleep(DataInterval)
			}
		}

		// read loop complete, exit
		break
	}
}

// loop listening for messages from the web interface
// send commands to the ecu if required via a go routine as to not block
func recieveMessageFromWebViewLoop(mems *rosco.MemsConnection) {
	for {
		m := <-webToMemsChannel
		c := evaluateCommand(m)

		switch c {
		case commandPauseDataLoop:
			{
				paused = true
				rosco.LogI.Printf("Paused Data Loop, sending heartbeats to keep connection alive")
			}
		case commandStartDataLoop:
			{
				paused = false
				rosco.LogI.Printf("Resuming Data Loop")
			}
		case commandResetECU:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Reset ECU")
				go sendCommandToMemsChannel(mems, rosco.MEMS_ResetECU)
			}
		case commandClearFaults:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Clear Faults")
				go sendCommandToMemsChannel(mems, rosco.MEMS_ClearFaults)
			}
		case commandResetAdjustments:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Reset Adjustments")
				go sendCommandToMemsChannel(mems, rosco.MEMS_ResetAdj)
			}
		case commandIncreaseIdleSpeed:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Increase Idle Speed")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IdleSpeed_Increment)
			}
		case commandIncreaseIdleHot:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Increase Idle Decay (Hot)")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IdleDecay_Increment)
			}
		case commandIncreaseFuelTrim:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Increase Fuel Trim (LTFT)")
				go sendCommandToMemsChannel(mems, rosco.MEMS_LTFT_Increment)
			}
		case commandIncreaseIgnitionAdvance:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Increase Ignition Advance")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IgnitionAdvanceOffset_Increment)
			}
		case commandDecreaseIdleSpeed:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Decrease Idle Speed")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IdleSpeed_Decrement)
			}
		case commandDecreaseIdleHot:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Decrease Idle Decay (Hot)")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IdleDecay_Decrement)
			}
		case commandDecreaseFuelTrim:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Decrease Fuel Trim (LTFT)")
				go sendCommandToMemsChannel(mems, rosco.MEMS_LTFT_Decrement)
			}
		case commandDecreaseIgnitionAdvance:
			{
				rosco.LogI.Printf("memsCommandResponseLoop sending Decrease Ignition Advance")
				go sendCommandToMemsChannel(mems, rosco.MEMS_IgnitionAdvanceOffset_Decrement)
			}
		default:
		}
	}
}

// send the command to be executed by the ECU via a channel
func sendCommandToMemsChannel(mems *rosco.MemsConnection, command []byte) {
	var m rosco.MemsCommandResponse
	m.Command = command

	mems.SendToECU <- m
}

// send a message back to the web interface via a channel
func sendDataToWebView(memsdata rosco.MemsData) {
	var m wsMsg

	m.Action = "data"

	data, _ := json.Marshal(memsdata)
	m.Data = string(data)
	memsToWebChannel <- m
}

// send configuration to the web interace via a channel
func sendConfigToWebView(config *rosco.ReadmemsConfig) {
	var m wsMsg

	m.Action = "config"

	data, _ := json.Marshal(config)
	m.Data = string(data)
	memsToWebChannel <- m
}

func getSerialPorts() []string {
	ports, err := serial.GetPortsList()

	if err != nil {
		rosco.LogI.Printf("error enumerating serial ports")
	}
	if len(ports) == 0 {
		rosco.LogW.Printf("unable to find any serial ports")
	}
	for _, port := range ports {
		rosco.LogI.Printf("found serial port %v", port)
	}

	return ports
}

func main() {
	var showHelp bool

	// create a map of commands
	createCommandMap()

	// use if the readmems config is supplied
	config = rosco.ReadConfig()

	// parse the command line parameters and override config file settings
	flag.StringVar(&config.Port, "port", config.Port, "Name/path of the serial port")
	flag.StringVar(&config.Command, "command", config.Command, "Command to send")
	flag.StringVar(&config.Loop, "loop", config.Loop, "Read loop count, 'inf' for infinite")
	flag.BoolVar(&showHelp, "help", false, "A brief help message")
	flag.Parse()

	if showHelp {
		rosco.LogI.Println(helpMessage())
		return
	}

	if config.Loop == "inf" {
		// infitite loop, so set loop count to a very big number
		config.Loop = "100000000"
	}

	// get the list of ports available
	config.Ports = append(config.Ports, config.Port)
	config.Ports = append(config.Ports, getSerialPorts()...)

	// run the http server
	go RunHTTPServer()
	go sendConfigToWebView(config)

	ShowWebView(config)
}
