package zigbee

import (
	"github.com/alivinco/conbee-ad/conbee"
	"github.com/alivinco/conbee-ad/model"
	"github.com/futurehomeno/fimpgo"
	log "github.com/sirupsen/logrus"
	"strings"
)

const ServiceName  = "conbee"

type FimpToConbeeRouter struct {
	inboundMsgCh fimpgo.MessageCh
	mqt          *fimpgo.MqttTransport
	instanceId   string
	conbeeClient *conbee.Client
	netService *NetworkService
	appLifecycle *model.Lifecycle
	configs      *model.Configs
}

func NewFimpToConbeeRouter(mqt *fimpgo.MqttTransport, conbeeClient *conbee.Client,netService *NetworkService,appLifecycle *model.Lifecycle,configs *model.Configs) *FimpToConbeeRouter {
	fc := FimpToConbeeRouter{inboundMsgCh: make(fimpgo.MessageCh,5),mqt:mqt,netService:netService,appLifecycle:appLifecycle,configs:configs}
	fc.mqt.RegisterChannel("ch1",fc.inboundMsgCh)
	fc.conbeeClient = conbeeClient
		return &fc
}

func (fc *FimpToConbeeRouter) Start() {
	fc.mqt.Subscribe("pt:j1/+/rt:dev/rn:conbee/ad:1/#")
	fc.mqt.Subscribe("pt:j1/+/rt:ad/rn:conbee/ad:1")
	go func(msgChan fimpgo.MessageCh) {
		for  {
			select {
			case newMsg :=<- msgChan:
				fc.routeFimpMessage(newMsg)

			}
		}

	}(fc.inboundMsgCh)
}

func (fc *FimpToConbeeRouter) routeFimpMessage(newMsg *fimpgo.Message) {
	log.Debug("New fimp msg")
	addr := strings.Replace(newMsg.Addr.ServiceAddress,"_0","",1)
	switch newMsg.Payload.Service {
	case "out_lvl_switch" :
		addr = strings.Replace(addr,"l","",1)
		switch newMsg.Payload.Type {
		case "cmd.binary.set":
			val,_ := newMsg.Payload.GetBoolValue()
			req := conbee.ConnbeeLightRequest{On:val}
			var resp interface{}
			log.Debug("Request ",req)
			_ , err :=  fc.conbeeClient.SendConbeeRequest("PUT","lights/"+addr+"/state",req,resp)
			if err != nil {
				log.Error("Response error ",err)
			}
			//log.Debug("Status code = ",respH.StatusCode)
		case "cmd.lvl.set":
			val,_ := newMsg.Payload.GetIntValue()
			// 255 - 100%
			// A   - x%
			//x = A * 100 / 255

			req := conbee.ConnbeeLightRequest{Bri:int(val),On:true}
			var resp interface{}
			log.Debug("Request ",req)
			_ , err :=  fc.conbeeClient.SendConbeeRequest("PUT","lights/"+addr+"/state",req,resp)
			if err != nil {
				log.Error("Response error ",err)
			}
		}

	case "out_bin_switch":
		log.Debug("Sending switch")
		val,_ := newMsg.Payload.GetBoolValue()
		req := conbee.ConnbeeLightRequest{On:val}
		var resp interface{}
		log.Debug("Request ",req)
		addr = strings.Replace(addr,"l","",1)
		respH , err :=  fc.conbeeClient.SendConbeeRequest("PUT","lights/"+addr+"/state",req,resp)
		if err != nil {
			log.Error("Response error ",err)
		}
		log.Debug("Status code = ",respH.StatusCode)

		//
	case ServiceName:
		adr := &fimpgo.Address{MsgType: fimpgo.MsgTypeEvt, ResourceType: fimpgo.ResourceTypeAdapter, ResourceName: ServiceName, ResourceAddress:"1"}
		switch newMsg.Payload.Type {
		case "cmd.auth.login":
			reqVal, err := newMsg.Payload.GetStrMapValue()
			status := "ok"
			if err != nil {
				log.Error("Incorrect login message ")
				return
			}
			username,_ := reqVal["username"]
			password,_ := reqVal["password"]
			if username != ""{
				err := fc.conbeeClient.Login(username,password)
				if err == nil {
					fc.configs.ConbeeApiKey = fc.conbeeClient.ApiKey()
					fc.appLifecycle.PublishEvent(model.EventConfigured,"fimp-conbee",nil)
				}
			}
			fc.configs.SaveToFile()
			if err != nil {
				status = "error"
			}
			msg := fimpgo.NewStringMessage("evt.system.login_report",ServiceName,status,nil,nil,newMsg.Payload)
			if err := fc.mqt.RespondToRequest(newMsg.Payload,msg); err != nil {
				fc.mqt.Publish(adr,msg)
			}
		case "cmd.system.get_connect_params":
			val := map[string]string{"host":fc.configs.ConbeeUrl,"username":"","password":""}
			msg := fimpgo.NewStrMapMessage("evt.system.connect_params_report",ServiceName,val,nil,nil,newMsg.Payload)
			if err := fc.mqt.RespondToRequest(newMsg.Payload,msg); err != nil {
				fc.mqt.Publish(adr,msg)
			}
		case "cmd.config.set":
			fallthrough
		case "cmd.system.connect":
			reqVal, err := newMsg.Payload.GetStrMapValue()
			var errStr string
			status := "ok"
			if err != nil {
				log.Error("Incorrect login message ")
				 errStr = err.Error()
			}
			host,_ := reqVal["host"]
			username,_ := reqVal["username"]
			password,_ := reqVal["password"]
			fc.configs.ConbeeUrl = host

			//if username != ""{
			err = fc.conbeeClient.Login(username,password)
			if err == nil {
				fc.configs.ConbeeApiKey = fc.conbeeClient.ApiKey()
				fc.appLifecycle.PublishEvent(model.EventConfigured,"fimp-conbee",nil)
			}else {
				log.Error("Login error :",err)
			}
			//}
			fc.configs.SaveToFile()
			if err != nil {
				status = "error"
			}
			val := map[string]string{"status":status,"error":errStr}
			msg := fimpgo.NewStrMapMessage("evt.system.connect_report",ServiceName,val,nil,nil,newMsg.Payload)
			if err := fc.mqt.RespondToRequest(newMsg.Payload,msg); err != nil {
				fc.mqt.Publish(adr,msg)
			}

		case "cmd.network.get_all_nodes":
			fc.netService.SendListOfDevices()
		case "cmd.thing.get_inclusion_report":
			nodeId , _ := newMsg.Payload.GetStringValue()
			var deviceType string
			if strings.Contains(nodeId,"s") {
				deviceType = "sensors"
				nodeId = strings.Replace(nodeId,"s","",1)
			}else {
				deviceType = "lights"
				nodeId = strings.Replace(nodeId,"l","",1)
			}

			fc.netService.SendInclusionReport(deviceType,nodeId)
		case "cmd.thing.inclusion":
			flag , _ := newMsg.Payload.GetBoolValue()
			fc.netService.OpenNetwork(flag)
		case "cmd.thing.delete":
			// remove device from network
			val,err := newMsg.Payload.GetStrMapValue()
			if err != nil {
				log.Error("Wrong msg format")
				return
			}
			deviceId , ok := val["address"]
			if ok {
				fc.netService.DeleteThing(deviceId)
			}else {
				log.Error("Incorrect address")

			}

		}
		//

	}

}


