package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/andrewdjackson/memsfcr/rosco"
	"github.com/andrewdjackson/memsfcr/ui"
	"github.com/andrewdjackson/memsfcr/utils"
	"github.com/zserge/webview"
)

// MemsReader structure
type MemsReader struct {
	wi  *ui.WebInterface
	fcr *ui.MemsFCR
}

// NewMemsReader creates an instance of a MEMs Reader
func NewMemsReader() *MemsReader {
	r := &MemsReader{}

	// create the Mems Fault Code Reader
	r.fcr = ui.NewMemsFCR()

	// create a mems instance and assign it to the fault code reader instance
	r.fcr.ECU = rosco.NewMemsConnection()

	// create and run the web interfacce
	r.wi = ui.NewWebInterface()
	utils.LogI.Printf("running web server %d", r.wi.HTTPPort)

	return r
}

// webLoop services the channels processing messages send from the web interface
// run as a goroutine
func (r *MemsReader) webMainLoop() {
	for {
		m := <-r.wi.FromWebChannel
		utils.LogI.Printf("%s received message FromWebChannel in main webLoop (%v)", utils.ReceiveFromWebTrace, m)

		// evalute the message sent from the web interface
		// and determine the action

		action := ui.EvaluateWebMsg(m)
		utils.LogI.Printf("evaluated action (%v) as %d", action.Msg, action.Value)

		switch action.Value {
		case ui.ConfigRead:
			r.sendConfigToWebView()

		case ui.ConnectECU:
			// connect the ECU
			utils.LogI.Printf("connecting ecu")
			if r.fcr.ConnectFCR() {
				r.sendConnectionStatusToWebView()
			}
		case ui.Dataframe:
			go r.fcr.TxECU(rosco.MEMS_DataFrame)
		case ui.PauseDataLoop:
			{
				//paused = true
				utils.LogI.Printf("Paused Data Loop, sending heartbeats to keep connection alive")
			}
		case ui.StartDataLoop:
			{
				//paused = false
				utils.LogI.Printf("Resuming Data Loop")
			}
		case ui.ResetECU:
			go r.fcr.TxECU(rosco.MEMS_ResetECU)
		case ui.ClearFaults:
			go r.fcr.TxECU(rosco.MEMS_ClearFaults)
		case ui.ResetAdjustments:
			go r.fcr.TxECU(rosco.MEMS_ResetAdj)
		case ui.IncreaseIdleSpeed:
			go r.fcr.TxECU(rosco.MEMS_IdleSpeed_Increment)
		case ui.IncreaseIdleHot:
			go r.fcr.TxECU(rosco.MEMS_IdleDecay_Increment)
		case ui.IncreaseFuelTrim:
			go r.fcr.TxECU(rosco.MEMS_LTFT_Increment)
		case ui.IncreaseIgnitionAdvance:
			go r.fcr.TxECU(rosco.MEMS_IgnitionAdvanceOffset_Increment)
		case ui.DecreaseIdleSpeed:
			go r.fcr.TxECU(rosco.MEMS_IdleSpeed_Decrement)
		case ui.DecreaseIdleHot:
			go r.fcr.TxECU(rosco.MEMS_IdleDecay_Decrement)
		case ui.DecreaseFuelTrim:
			go r.fcr.TxECU(rosco.MEMS_LTFT_Decrement)
		case ui.DecreaseIgnitionAdvance:
			go r.fcr.TxECU(rosco.MEMS_IgnitionAdvanceOffset_Decrement)

		default:
		}
	}
}

func (r *MemsReader) sendConfigToWebView() {
	// pass configuration to the web interface
	m := ui.WebMsg{}
	m.Action = ui.WebActionConfig
	data, _ := json.Marshal(r.fcr.Config)
	m.Data = string(data)
	r.wi.ToWebChannel <- m
}

// send a connection status message back to the web interface via a channel
func (r *MemsReader) sendConnectionStatusToWebView() {
	var c rosco.MemsConnectionStatus
	var m ui.WebMsg

	c.Connected = r.fcr.ECU.Connected
	c.Initialised = r.fcr.ECU.Initialised

	m.Action = ui.WebActionConnection

	data, _ := json.Marshal(c)
	m.Data = string(data)

	r.wi.ToWebChannel <- m
	utils.LogI.Printf("%s sent connection status to web with ToWebChannel channel", utils.SendToWebTrace)
}

func (r *MemsReader) fcrMainLoop() {
	var data []byte

	// busy clearing channels
	for {
		m := <-r.fcr.ECUSendToFCR
		utils.LogI.Printf("%s (Rx.3) received message ECUSendToFCR (%v)", utils.ReceiveFromWebTrace, m)

		// send to the web
		df := ui.WebMsg{}

		if bytes.Compare(m.Command, rosco.MEMS_DataFrame) == 0 {
			// dataframe command
			df.Action = ui.WebActionData
			data, _ = json.Marshal(m.MemsDataFrame)
		} else {
			df.Action = ui.WebActionECUResponse
			data, _ = json.Marshal(m.Response)
		}

		df.Data = string(data)

		select {
		case r.wi.ToWebChannel <- df:
		default:
		}
	}
}

// displayWebView creates a webview
// this must be run in the main thread
func displayWebView(wi *ui.WebInterface) {
	w := webview.New(true)
	defer w.Destroy()

	w.SetTitle("MEMS Fault Code Reader")
	w.SetSize(1120, 920, webview.HintNone)

	w.Bind("quit", func() {
		w.Terminate()
	})

	url := fmt.Sprintf("http://127.0.0.1:%d/public/html/index.html", wi.HTTPPort)

	w.Navigate(url)
	w.Run()
}

func main() {
	memsReader := NewMemsReader()

	go memsReader.wi.RunHTTPServer()
	go memsReader.webMainLoop()
	go memsReader.fcrMainLoop()
	go memsReader.fcr.TxRxECULoop()

	// run the listener for messages sent to the web interface from
	// the backend application
	go memsReader.wi.ListenToWebChannelLoop()

	// display the web interface
	displayWebView(memsReader.wi)
}