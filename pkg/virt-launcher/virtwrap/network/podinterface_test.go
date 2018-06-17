/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package network

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubevirt.io/kubevirt/pkg/api/v1"
	"kubevirt.io/kubevirt/pkg/log"
	"kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

var _ = Describe("Pod Network", func() {
	var mockNetwork *MockNetworkHandler
	var ctrl *gomock.Controller
	var dummy *netlink.Dummy
	var addrList []netlink.Addr
	var routeList []netlink.Route
	var routeAddr netlink.Route
	var fakeMac net.HardwareAddr
	var fakeAddr netlink.Addr
	var updateFakeMac net.HardwareAddr
	var bridgeTest *netlink.Bridge
	var bridgeAddr *netlink.Addr
	var testNic *VIF
	var interfaceXml []byte
	var tmpDir string
	var dnsname string
	// var iface *v1.Interface
	// var network *v1.Network
	// var slirpConfig api.Arg
	log.Log.SetIOWriter(GinkgoWriter)

	_, dnsnamelist, _ := getResolvConfDetailsFromPod()
	for _, dnsSearchName := range dnsnamelist {
		dnsname += fmt.Sprintf(",dnssearch=%s", dnsSearchName)
	}

	BeforeEach(func() {
		tmpDir, _ := ioutil.TempDir("", "networktest")
		setInterfaceCacheFile(tmpDir + "/cache-%s.json")

		ctrl = gomock.NewController(GinkgoT())
		mockNetwork = NewMockNetworkHandler(ctrl)
		Handler = mockNetwork
		testMac := "12:34:56:78:9A:BC"
		updateTestMac := "AF:B3:1F:78:2A:CA"
		dummy = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: 1}}
		address := &net.IPNet{IP: net.IPv4(10, 35, 0, 6), Mask: net.CIDRMask(24, 32)}
		gw := net.IPv4(10, 35, 0, 1)
		fakeMac, _ = net.ParseMAC(testMac)
		updateFakeMac, _ = net.ParseMAC(updateTestMac)
		fakeAddr = netlink.Addr{IPNet: address}
		addrList = []netlink.Addr{fakeAddr}
		routeAddr = netlink.Route{Gw: gw}
		routeList = []netlink.Route{routeAddr}
		// iface = &v1.Interface{Name: "testnet"}
		// network = &v1.Network{Name: "testnet", NetworkSource: v1.NetworkSource{Pod: &v1.PodNetwork{}}}
		// slirpConfig = api.Arg{Value: fmt.Sprintf("user,id=%s", iface.Name)}

		// Create a bridge
		bridgeTest = &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name: api.DefaultBridgeName,
			},
		}

		bridgeAddr, _ = netlink.ParseAddr(bridgeFakeIP)
		testNic = &VIF{Name: podInterface,
			IP:      fakeAddr,
			MAC:     fakeMac,
			Gateway: gw}
		interfaceXml = []byte(`<Interface type="bridge"><source bridge="br1"></source><model type="virtio"></model><mac address="12:34:56:78:9a:bc"></mac><alias name="default"></alias></Interface>`)
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	TestPodInterfaceIPBinding := func(vm *v1.VirtualMachineInstance, domain *api.Domain) {

		mockNetwork.EXPECT().LinkByName(podInterface).Return(dummy, nil)
		mockNetwork.EXPECT().AddrList(dummy, netlink.FAMILY_V4).Return(addrList, nil)
		mockNetwork.EXPECT().RouteList(dummy, netlink.FAMILY_V4).Return(routeList, nil)
		mockNetwork.EXPECT().GetMacDetails(podInterface).Return(fakeMac, nil)
		mockNetwork.EXPECT().AddrDel(dummy, &fakeAddr).Return(nil)
		mockNetwork.EXPECT().LinkSetDown(dummy).Return(nil)
		mockNetwork.EXPECT().SetRandomMac(podInterface).Return(updateFakeMac, nil)
		mockNetwork.EXPECT().LinkSetUp(dummy).Return(nil)
		mockNetwork.EXPECT().LinkAdd(bridgeTest).Return(nil)
		mockNetwork.EXPECT().LinkByName(api.DefaultBridgeName).Return(bridgeTest, nil)
		mockNetwork.EXPECT().LinkSetUp(bridgeTest).Return(nil)
		mockNetwork.EXPECT().ParseAddr(bridgeFakeIP).Return(bridgeAddr, nil)
		mockNetwork.EXPECT().AddrAdd(bridgeTest, bridgeAddr).Return(nil)
		mockNetwork.EXPECT().StartDHCP(testNic, bridgeAddr)

		err := SetupPodNetwork(vm, domain)
		Expect(err).To(BeNil())
		Expect(len(domain.Spec.Devices.Interfaces)).To(Equal(1))
		xmlStr, err := xml.Marshal(domain.Spec.Devices.Interfaces)
		Expect(string(xmlStr)).To(Equal(string(interfaceXml)))
		Expect(err).To(BeNil())

		// Calling SetupPodNetwork a second time should result in no
		// mockNetwork function calls and interface should be identical
		err = SetupPodNetwork(vm, domain)

		Expect(err).To(BeNil())
		Expect(len(domain.Spec.Devices.Interfaces)).To(Equal(1))
		xmlStr, err = xml.Marshal(domain.Spec.Devices.Interfaces)
		Expect(string(xmlStr)).To(Equal(string(interfaceXml)))
		Expect(err).To(BeNil())
	}

	Context("on successful setup", func() {
		It("should define a new VIF bind to a bridge", func() {

			domain := NewDomainWithPodNetwork()
			vm := newVM("testnamespace", "testVmName")

			api.SetObjectDefaults_Domain(domain)
			TestPodInterfaceIPBinding(vm, domain)
		})
		It("should panic if pod networking fails to setup", func() {
			testNetworkPanic := func() {
				domain := NewDomainWithPodNetwork()
				vm := newVM("testnamespace", "testVmName")

				api.SetObjectDefaults_Domain(domain)

				mockNetwork.EXPECT().LinkByName(podInterface).Return(dummy, nil)
				mockNetwork.EXPECT().AddrList(dummy, netlink.FAMILY_V4).Return(addrList, nil)
				mockNetwork.EXPECT().RouteList(dummy, netlink.FAMILY_V4).Return(routeList, nil)
				mockNetwork.EXPECT().GetMacDetails(podInterface).Return(fakeMac, nil)
				mockNetwork.EXPECT().AddrDel(dummy, &fakeAddr).Return(errors.New("device is busy"))

				SetupPodNetwork(vm, domain)
			}
			Expect(testNetworkPanic).To(Panic())
		})
		Context("func filterPodNetworkRoutes()", func() {
			defRoute := netlink.Route{
				Gw: net.IPv4(10, 35, 0, 1),
			}
			staticRoute := netlink.Route{
				Dst: &net.IPNet{IP: net.IPv4(10, 45, 0, 10), Mask: net.CIDRMask(32, 32)},
				Gw:  net.IPv4(10, 25, 0, 1),
			}
			gwRoute := netlink.Route{
				Dst: &net.IPNet{IP: net.IPv4(10, 35, 0, 1), Mask: net.CIDRMask(32, 32)},
			}
			nicRoute := netlink.Route{Src: net.IPv4(10, 35, 0, 6)}
			emptyRoute := netlink.Route{}
			staticRouteList := []netlink.Route{defRoute, gwRoute, nicRoute, emptyRoute, staticRoute}

			It("should remove empty routes, and routes matching nic, leaving others intact", func() {
				expectedRouteList := []netlink.Route{defRoute, gwRoute, staticRoute}
				Expect(filterPodNetworkRoutes(staticRouteList, testNic)).To(Equal(expectedRouteList))
			})
		})
		Context("func findInterfaceByName()", func() {
			It("should fail on empty interface list", func() {
				_, err := findInterfaceByName([]api.Interface{}, "default")
				Expect(err).To(HaveOccurred())
			})
			It("should fail when interface is missing", func() {
				interfaces := []api.Interface{
					api.Interface{
						Type: "not-bridge",
						Source: api.InterfaceSource{
							Bridge: api.DefaultBridgeName,
						},
						Alias: &api.Alias{
							Name: "iface1",
						},
					},
					api.Interface{
						Type: "bridge",
						Source: api.InterfaceSource{
							Bridge: "other_br",
						},
						Alias: &api.Alias{
							Name: "iface2",
						},
					},
				}
				_, err := findInterfaceByName(interfaces, "iface3")
				Expect(err).To(HaveOccurred())
			})
			It("should pass when interface alias matches the name", func() {
				interfaces := []api.Interface{
					api.Interface{
						Type: "bridge",
						Source: api.InterfaceSource{
							Bridge: api.DefaultBridgeName,
						},
						Alias: &api.Alias{
							Name: "iface1",
						},
					},
				}
				idx, err := findInterfaceByName(interfaces, "iface1")
				Expect(err).ToNot(HaveOccurred())
				Expect(idx).To(Equal(0))
			})
			It("should pass when matched interface is not the first in the list", func() {
				interfaces := []api.Interface{
					api.Interface{
						Type: "not-bridge",
						Source: api.InterfaceSource{
							Bridge: api.DefaultBridgeName,
						},
						Alias: &api.Alias{
							Name: "iface1",
						},
					},
					api.Interface{
						Type: "bridge",
						Source: api.InterfaceSource{
							Bridge: "other_br",
						},
						Alias: &api.Alias{
							Name: "iface2",
						},
					},
					api.Interface{
						Type: "bridge",
						Source: api.InterfaceSource{
							Bridge: api.DefaultBridgeName,
						},
						Alias: &api.Alias{
							Name: "iface3",
						},
					},
				}
				idx, err := findInterfaceByName(interfaces, "iface3")
				Expect(err).ToNot(HaveOccurred())
				Expect(idx).To(Equal(2))
			})
		})
	})
})

func newVM(namespace string, name string) *v1.VirtualMachineInstance {
	vmi := &v1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.VirtualMachineInstanceSpec{
			Domain:   v1.NewMinimalDomainSpec(),
			Networks: []v1.Network{*v1.DefaultPodNetwork()},
		},
	}
	vmi.Spec.Domain.Devices.Interfaces = []v1.Interface{*v1.DefaultNetworkInterface()}
	v1.SetObjectDefaults_VirtualMachineInstance(vmi)
	return vmi
}

func NewDomainWithPodNetwork() *api.Domain {

	domain := &api.Domain{}
	domain.Spec.Devices.Interfaces = []api.Interface{{
		Model: &api.Model{
			Type: "virtio",
		},
		Type: "bridge",
		Source: api.InterfaceSource{
			Bridge: api.DefaultBridgeName,
		},
		Alias: &api.Alias{
			Name: "default",
		}},
	}
	return domain
}

func NewDomainWithSlirpNetwork() *api.Domain {
	domain := &api.Domain{}

	if domain.Spec.QEMUCmd == nil {
		domain.Spec.QEMUCmd = &api.Commandline{}
	}

	if domain.Spec.QEMUCmd.QEMUArg == nil {
		domain.Spec.QEMUCmd.QEMUArg = make([]api.Arg, 0)
	}

	domain.Spec.QEMUCmd.QEMUArg = append(domain.Spec.QEMUCmd.QEMUArg, api.Arg{Value: "-device"})
	domain.Spec.QEMUCmd.QEMUArg = append(domain.Spec.QEMUCmd.QEMUArg, api.Arg{Value: fmt.Sprintf("%s,netdev=%s", "virtio", "testnet")})

	return domain
}
