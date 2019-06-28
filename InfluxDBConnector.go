/*
Copyright (c) 2019 Intel Corporation.

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

Explicit permissions are required to publish, distribute, sublicense, and/or sell copies of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package main

import (
	"os"
	"strings"
	"flag"

	eismsgbus "EISMessageBus/eismsgbus"
	common "IEdgeInsights/InfluxDBConnector/common"
	configManager "IEdgeInsights/InfluxDBConnector/configManager"
	dbManager "IEdgeInsights/InfluxDBConnector/dbManager"
	pubManager "IEdgeInsights/InfluxDBConnector/pubManager"
	subManager "IEdgeInsights/InfluxDBConnector/subManager"

	"github.com/golang/glog"
)

const (
	subServPort = "61971"
	configJSONPath     = "/IEI/go/src/IEdgeInsights/InfluxDBConnector/config/Config.json"
        subServHost = "localhost"
)


//Creating an object for InfluxDB Manager
var InfluxObj dbManager.InfluxDBManager

var pubMgr pubManager.PubManager
var credConfig common.DbCredential
var runtimeInfo common.ContainerConfig

//Function to read the DB credential and container runtime info from the config file
func readConfig() {
	var errConfig error
	var errRuntimeInfo error
	credConfig, errConfig = configManager.ReadInfluxConfig()
	if errConfig != nil {
		glog.Error("Error in reading the DB credentials : %v" + errConfig.Error())
		os.Exit(-1)
	}

	runtimeInfo, errRuntimeInfo = configManager.ReadContainerInfo()
	if errRuntimeInfo != nil {
		glog.Error("Error in reading the Runtime Info : %v" + errRuntimeInfo.Error())
		os.Exit(-1)
	}
}

//StartDb Function to start Influx Database
//Initialize the Influx database with the configurations
func StartDb() {
	InfluxObj.DbInfo = credConfig
	InfluxObj.CnInfo = runtimeInfo
	err := InfluxObj.Init()
	if err != nil {
		glog.Errorf("StartDb: Failed to initialize InfluxDB : %v", err)
		os.Exit(-1)
	}

	err = InfluxObj.CreateDataBase(InfluxObj.DbInfo.Database, InfluxObj.DbInfo.Retention)
	if err != nil {
		glog.Errorf("StartDb: Failed to create database : %v", err)
		os.Exit(-1)
	}
}

// StartPublisher function to register the publisher and subscribe to influxdb
// ZeroMQ interface
func StartPublisher() {

	keywords := os.Getenv("PubTopics")
	keyword := strings.Split(keywords, ",")
	pubMgr.Init()
	pubMgr.RegFilter(&InfluxObj)

	for _, key := range keyword {
		glog.Infof("Publisher topic is : %s", key)
		pubMgr.RegPublisherList(key)
		cConfigList := configManager.ReadClientConfig(key, "pub")

		if cConfigList != nil {
			pubMgr.RegClientList(key)
			pubMgr.CreateClient(key, cConfigList)
		}

	}

	pubMgr.StartAllPublishers()
	var SubObj common.SubScriptionInfo
	SubObj.DbName = InfluxObj.DbInfo.Database
	SubObj.Host = subServHost
	SubObj.Port = subServPort

	// Subscribe to the influxdb database
	err := InfluxObj.Subscribe(SubObj, &pubMgr)
	if err != nil {
		glog.Errorf("StartPublisher: Failed to subscribe InfluxDB : %v", err)
		os.Exit(-1)
	}

}

//StartSubscriber Function to start the subscriber and insert data to influxdb
func StartSubscriber() {

	keywords := os.Getenv("SubTopics")
	keyword := strings.Split(keywords, ",")

	var subMgr subManager.SubManager
	var influxWrite dbManager.InfluxWriter
	influxWrite.DbInfo = credConfig
	influxWrite.CnInfo = runtimeInfo
	subMgr.Init()

	for _, key := range keyword {
		glog.Infof("Subscriber topic is : %s", key)

		subMgr.RegSubscriberList(key)
		cConfigList := configManager.ReadClientConfig(key, "sub")

		if cConfigList != nil {
			subMgr.RegClientList(key)
			subMgr.CreateClient(key, cConfigList)
		}
	}

	subMgr.StartAllSubscribers()
	subMgr.ReceiveFromAll(&influxWrite)
}

//Function to start the query server
func startReqReply() {
	keyword := os.Getenv("AppName")

	glog.Infof("Query topic is : %s", keyword)

	cConfigList := configManager.ReadClientConfig(keyword, "service")

	client, err := eismsgbus.NewMsgbusClient(cConfigList)
	if err != nil {
		glog.Errorf("-- Error initializing message bus context: %v\n", err)
		return
	}
	service, err := client.NewService(keyword)
	if err != nil {
		glog.Errorf("-- Error initializing service: %v\n", err)
		return
	}

	var influxQuery dbManager.InfluxQuery
	influxQuery.DbInfo = credConfig
	influxQuery.CnInfo = runtimeInfo

	flag := true

	for flag {
		msg, err := service.ReceiveRequest(-1)
		if err != nil {
			glog.Errorf("-- Error receiving request: %v\n", err)
			return
		}
		glog.Infof("Command receivce: %s", msg)
		response, _ := influxQuery.QueryInflux(msg)
		service.Response(response.Blob)
	}

}

//Function to stop the publishers
func cleanup() {
    pubMgr.StopAllClient()
    pubMgr.StopAllPublisher()
}


func main() {
	flag.Parse()
	done := make(chan bool)
	readConfig()
	StartDb()
	StartPublisher()
	StartSubscriber()
	go startReqReply()
	<-done
	cleanup()
}
