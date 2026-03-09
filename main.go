// Copyright \d{4} VK Cloud.
//
// All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package main

import (
	"context"
	"log/slog"
	"net"
	"os"

	"github.com/davecgh/go-spew/spew"
)

func getHostFromArgs() string {
	if len(os.Args) < 2 {
		return ""
	}

	return os.Args[1]
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	host := getHostFromArgs()
	if host == "" {
		panic("no host specified")
	}

	ip := net.ParseIP(host)
	if ip == nil {
		panic("invalid IP address")
	}

	// port := 623
	username := "ADMIN"
	password := "Admin@DM1N"

	// collector := NewIPMICollector(logger)
	// collector := NewRedfishCollector(logger)
	//
	// result := UnionCollectResult{}
	//
	// err := collector.CollectRemote(context.Background(), &result, ip, &IdentifyOptions{
	// 	Credentials: ServerCredentials{
	// 		Username: username,
	// 		Password: password,
	// 	},
	// 	IPMIConfig: IPMIConfig{
	// 		Port: 623,
	// 	},
	//
	// 	RedfishConfig: RedfishConfig{
	// 		Port: 9117,
	// 		// Port:     9011,
	// 		Prefix:   "",
	// 		Insecure: true,
	// 	},
	// })
	// if err != nil {
	// 	panic(err)
	// }

	identifier := NewServerIdentifier(logger)
	res, err := identifier.IdentifyRemote(context.Background(), ip, IdentifyOptions{
		Credentials: ServerCredentials{
			Username: username,
			Password: password,
		},
		IPMIConfig: IPMIConfig{
			Port: 623,
		},

		RedfishConfig: RedfishConfig{
			Port: 9117,
			// Port:     9011,
			Prefix:   "",
			Insecure: true,
		},
	})
	if err != nil {
		panic(err)
	}

	// fmt.Printf("%v", result)
	spew.Dump(res)

	// client, err := ipmi.NewClient(host, port, username, password)
	// if err != nil {
	// 	panic(err)
	// }
	//
	// // client.WithDebug(true)
	// client.WithInterface(ipmi.InterfaceLanplus)
	//
	// ctx := context.Background()
	//
	// if err := client.Connect(ctx); err != nil {
	// 	panic(err)
	// }
	//
	// if _, err := getBMCMac(ctx, client); err != nil {
	// 	panic(err)
	// }
	//
	// res, err := client.GetDeviceID(ctx)
	// if err != nil {
	// 	panic(err)
	// }
	//
	// fmt.Println("DeviceId", res.DeviceID)
	//
	// // https://www.iana.org/assignments/enterprise-numbers/enterprise-numbers
	// fmt.Println("ManufacturerID", res.ManufacturerID)
	// fmt.Println("Manufacturer", getVendorName(res.ManufacturerID))
	// fmt.Println("-----------------")
	//
	// resFru, err := client.GetFRU(ctx, 0, "")
	// if err != nil {
	//
	// 	fmt.Println("Error getting FRU", err)
	// 	_, err := ReadFRUSafe(ctx, client, 0)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// }
	//
	// if !resFru.Present() {
	// 	panic("FRU device not found")
	// }
	//
	// if resFru.BoardInfoArea != nil {
	// 	fmt.Println("Board Manufacturer", string(resFru.BoardInfoArea.Manufacturer))
	// 	fmt.Println("Board Product", string(resFru.BoardInfoArea.ProductName))
	// }
	//
	// if resFru.ProductInfoArea != nil {
	// 	fmt.Println("Product Manufacturer", string(resFru.ProductInfoArea.Manufacturer))
	// 	fmt.Println("Product Name", string(resFru.ProductInfoArea.Name))
	// }
	//
	// fmt.Println("-----------------")
	//
	// fmt.Println()
}
