// Copyright © 2018 The Things Network Foundation, The Things Industries B.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package applicationserver_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pbtypes "github.com/gogo/protobuf/types"
	"github.com/smartystreets/assertions"
	"go.thethings.network/lorawan-stack/pkg/applicationserver"
	"go.thethings.network/lorawan-stack/pkg/component"
	"go.thethings.network/lorawan-stack/pkg/config"
	"go.thethings.network/lorawan-stack/pkg/errors"
	"go.thethings.network/lorawan-stack/pkg/rpcmetadata"
	"go.thethings.network/lorawan-stack/pkg/ttnpb"
	"go.thethings.network/lorawan-stack/pkg/types"
	"go.thethings.network/lorawan-stack/pkg/util/test"
	"go.thethings.network/lorawan-stack/pkg/util/test/assertions/should"
	"google.golang.org/grpc"
)

type connChannels struct {
	up          chan *ttnpb.ApplicationUp
	downPush    chan *ttnpb.DownlinkQueueRequest
	downReplace chan *ttnpb.DownlinkQueueRequest
	downErr     chan error
}

func TestApplicationServer(t *testing.T) {
	a := assertions.New(t)

	ctx := test.Context()
	is, isAddr := startMockIS(ctx)
	js, jsAddr := startMockJS(ctx)
	ns, nsAddr := startMockNS(ctx)

	// Register the application in the Entity Registry.
	is.add(ctx, registeredApplicationID, registeredApplicationKey)

	// Register some sessions in the Join Server. Sometimes the keys are sent by the Network Server as part of the
	// join-accept, and sometimes they are not sent by the Network Server so the Application Server gets them from the
	// Join Server.
	js.add(ctx, *registeredDevice.DevEUI, "session1", ttnpb.KeyEnvelope{
		// AppSKey is []byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11}.
		Key:      []byte{0xa8, 0x11, 0x8f, 0x80, 0x2e, 0xbf, 0x8, 0xdc, 0x62, 0x37, 0xc3, 0x4, 0x63, 0xa2, 0xfa, 0xcb, 0xf8, 0x87, 0xaa, 0x31, 0x90, 0x23, 0x85, 0xc1},
		KEKLabel: "test",
	})
	js.add(ctx, *registeredDevice.DevEUI, "session2", ttnpb.KeyEnvelope{
		// AppSKey is []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22}
		Key:      []byte{0x39, 0x11, 0x40, 0x98, 0xa1, 0x5d, 0x6f, 0x92, 0xd7, 0xf0, 0x13, 0x21, 0x5b, 0x5b, 0x41, 0xa8, 0x98, 0x2d, 0xac, 0x59, 0x34, 0x76, 0x36, 0x18},
		KEKLabel: "test",
	})
	js.add(ctx, *registeredDevice.DevEUI, "session3", ttnpb.KeyEnvelope{
		// AppSKey is []byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33}
		Key:      []byte{0x5, 0x81, 0xe1, 0x15, 0x8a, 0xc3, 0x13, 0x68, 0x5e, 0x8d, 0x15, 0xc0, 0x11, 0x92, 0x14, 0x49, 0x9f, 0xa0, 0xc6, 0xf1, 0xdb, 0x95, 0xff, 0xbd},
		KEKLabel: "test",
	})
	js.add(ctx, *registeredDevice.DevEUI, "session4", ttnpb.KeyEnvelope{
		// AppSKey is []byte{0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44}
		Key:      []byte{0x30, 0xcf, 0x47, 0x91, 0x11, 0x64, 0x53, 0x3f, 0xc3, 0xd5, 0xd8, 0x56, 0x5b, 0x71, 0xcb, 0xe7, 0x6d, 0x14, 0x2b, 0x2c, 0xf2, 0xc2, 0xd7, 0x7b},
		KEKLabel: "test",
	})

	deviceRegistry := newMemDeviceRegistry()
	linkRegistry := newMemLinkRegistry()
	linkRegistry.Set(ctx, registeredApplicationID, nil, func(_ *ttnpb.ApplicationLink) (*ttnpb.ApplicationLink, []string, error) {
		return &ttnpb.ApplicationLink{
			DefaultFormatters: &ttnpb.MessagePayloadFormatters{
				UpFormatter:   registeredApplicationFormatter,
				DownFormatter: registeredApplicationFormatter,
			},
		}, []string{"default_formatters"}, nil
	})

	c := component.MustNew(test.GetLogger(t), &component.Config{
		ServiceBase: config.ServiceBase{
			GRPC: config.GRPC{
				Listen:                      ":9184",
				AllowInsecureForCredentials: true,
			},
			Cluster: config.Cluster{
				IdentityServer: isAddr,
				JoinServer:     jsAddr,
				NetworkServer:  nsAddr,
			},
			KeyVault: config.KeyVault{
				Static: map[string][]byte{
					"test": {0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
				},
			},
		},
	})
	config := &applicationserver.Config{
		LinkMode: "all",
		Devices:  deviceRegistry,
		Links:    linkRegistry,
		DeviceRepository: applicationserver.DeviceRepositoryConfig{
			Static: deviceRepositoryData,
		},
	}
	as, err := applicationserver.New(c, config)
	if !a.So(err, should.BeNil) {
		t.FailNow()
	}

	roles := as.Roles()
	a.So(len(roles), should.Equal, 1)
	a.So(roles[0], should.Equal, ttnpb.PeerInfo_APPLICATION_SERVER)

	test.Must(nil, c.Start())
	defer c.Close()
	mustHavePeer(ctx, c, ttnpb.PeerInfo_NETWORK_SERVER)
	mustHavePeer(ctx, c, ttnpb.PeerInfo_JOIN_SERVER)
	mustHavePeer(ctx, c, ttnpb.PeerInfo_ENTITY_REGISTRY)

	for _, ptc := range []struct {
		Protocol  string
		ValidAuth func(ctx context.Context, ids ttnpb.ApplicationIdentifiers, key string) bool
		Connect   func(ctx context.Context, t *testing.T, ids ttnpb.ApplicationIdentifiers, key string, chs *connChannels) error
	}{
		{
			Protocol: "grpc",
			ValidAuth: func(ctx context.Context, ids ttnpb.ApplicationIdentifiers, key string) bool {
				return ids == registeredApplicationID && key == registeredApplicationKey
			},
			Connect: func(ctx context.Context, t *testing.T, ids ttnpb.ApplicationIdentifiers, key string, chs *connChannels) error {
				conn, err := grpc.Dial(":9184", grpc.WithInsecure(), grpc.WithBlock())
				if err != nil {
					return err
				}
				defer conn.Close()
				creds := grpc.PerRPCCredentials(rpcmetadata.MD{
					AuthType:      "Key",
					AuthValue:     key,
					AllowInsecure: true,
				})
				client := ttnpb.NewAppAsClient(conn)
				stream, err := client.Subscribe(ctx, &ids, creds)
				if err != nil {
					return err
				}
				errCh := make(chan error, 1)
				// Read upstream.
				go func() {
					for {
						msg, err := stream.Recv()
						if err != nil {
							errCh <- err
							return
						}
						chs.up <- msg
					}
				}()
				// Write downstream.
				go func() {
					for {
						var err error
						select {
						case req := <-chs.downPush:
							_, err = client.DownlinkQueuePush(ctx, req, creds)
						case req := <-chs.downReplace:
							_, err = client.DownlinkQueueReplace(ctx, req, creds)
						}
						chs.downErr <- err
					}
				}()
				select {
				case err := <-errCh:
					return err
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		},
	} {
		t.Run(fmt.Sprintf("Authenticate/%v", ptc.Protocol), func(t *testing.T) {
			for _, ctc := range []struct {
				Name string
				ID   ttnpb.ApplicationIdentifiers
				Key  string
			}{
				{
					Name: "ValidIDAndKey",
					ID:   registeredApplicationID,
					Key:  registeredApplicationKey,
				},
				{
					Name: "InvalidKey",
					ID:   registeredApplicationID,
					Key:  "invalid-key",
				},
				{
					Name: "InvalidIDAndKey",
					ID:   ttnpb.ApplicationIdentifiers{ApplicationID: "invalid-gateway"},
					Key:  "invalid-key",
				},
			} {
				t.Run(ctc.Name, func(t *testing.T) {
					ctx, cancel := context.WithDeadline(ctx, time.Now().Add(timeout))
					chs := &connChannels{
						up:          make(chan *ttnpb.ApplicationUp),
						downPush:    make(chan *ttnpb.DownlinkQueueRequest),
						downReplace: make(chan *ttnpb.DownlinkQueueRequest),
						downErr:     make(chan error),
					}
					err := ptc.Connect(ctx, t, ctc.ID, ctc.Key, chs)
					cancel()
					if errors.IsDeadlineExceeded(err) {
						if !ptc.ValidAuth(ctx, ctc.ID, ctc.Key) {
							t.Fatal("Expected link error due to invalid auth")
						}
					} else if ptc.ValidAuth(ctx, ctc.ID, ctc.Key) {
						t.Fatalf("Expected deadline exceeded with valid auth, but have %v", err)
					}
				})
			}
		})

		t.Run(fmt.Sprintf("Traffic/%v", ptc.Protocol), func(t *testing.T) {
			ctx, cancel := context.WithCancel(ctx)
			chs := &connChannels{
				up:          make(chan *ttnpb.ApplicationUp),
				downPush:    make(chan *ttnpb.DownlinkQueueRequest),
				downReplace: make(chan *ttnpb.DownlinkQueueRequest),
				downErr:     make(chan error),
			}

			wg := &sync.WaitGroup{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := ptc.Connect(ctx, t, registeredApplicationID, registeredApplicationKey, chs)
				if !errors.IsCanceled(err) {
					t.Fatalf("Expected context canceled, but have %v", err)
				}
			}()
			// Wait for connection to establish.
			time.Sleep(timeout)

			t.Run("Upstream", func(t *testing.T) {
				ns.reset()
				deviceRegistry.Reset()
				deviceRegistry.Set(ctx, registeredDevice.EndDeviceIdentifiers, nil, func(_ *ttnpb.EndDevice) (*ttnpb.EndDevice, []string, error) {
					return registeredDevice, []string{"ids", "version_ids", "formatters"}, nil
				})

				for _, tc := range []struct {
					Name         string
					IDs          ttnpb.EndDeviceIdentifiers
					Message      *ttnpb.ApplicationUp
					AssertUp     func(t *testing.T, up *ttnpb.ApplicationUp)
					AssertDevice func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink)
				}{
					{
						Name: "RegisteredDevice/JoinAccept",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x11, 0x11, 0x11, 0x11}),
							Up: &ttnpb.ApplicationUp_JoinAccept{
								JoinAccept: &ttnpb.ApplicationJoinAccept{
									SessionKeyID: "session1",
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x11, 0x11, 0x11, 0x11}),
								Up: &ttnpb.ApplicationUp_JoinAccept{
									JoinAccept: &ttnpb.ApplicationJoinAccept{
										SessionKeyID: "session1",
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x11, 0x11, 0x11, 0x11},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session1",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0xa8, 0x11, 0x8f, 0x80, 0x2e, 0xbf, 0x8, 0xdc, 0x62, 0x37, 0xc3, 0x4, 0x63, 0xa2, 0xfa, 0xcb, 0xf8, 0x87, 0xaa, 0x31, 0x90, 0x23, 0x85, 0xc1},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 0,
								StartedAt:     dev.Session.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(dev.NextSession, should.BeNil)
							a.So(queue, should.HaveLength, 0)
						},
					},
					{
						Name: "RegisteredDevice/JoinAccept/WithAppSKey",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x22, 0x22, 0x22, 0x22}),
							Up: &ttnpb.ApplicationUp_JoinAccept{
								JoinAccept: &ttnpb.ApplicationJoinAccept{
									SessionKeyID: "session2",
									AppSKey: &ttnpb.KeyEnvelope{
										// AppSKey is []byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22}
										Key:      []byte{0x39, 0x11, 0x40, 0x98, 0xa1, 0x5d, 0x6f, 0x92, 0xd7, 0xf0, 0x13, 0x21, 0x5b, 0x5b, 0x41, 0xa8, 0x98, 0x2d, 0xac, 0x59, 0x34, 0x76, 0x36, 0x18},
										KEKLabel: "test",
									},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x22, 0x22, 0x22, 0x22}),
								Up: &ttnpb.ApplicationUp_JoinAccept{
									JoinAccept: &ttnpb.ApplicationJoinAccept{
										SessionKeyID: "session2",
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x22, 0x22, 0x22, 0x22},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session2",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x39, 0x11, 0x40, 0x98, 0xa1, 0x5d, 0x6f, 0x92, 0xd7, 0xf0, 0x13, 0x21, 0x5b, 0x5b, 0x41, 0xa8, 0x98, 0x2d, 0xac, 0x59, 0x34, 0x76, 0x36, 0x18},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 0,
								StartedAt:     dev.Session.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(dev.NextSession, should.BeNil)
							a.So(queue, should.HaveLength, 0)
						},
					},
					{
						Name: "RegisteredDevice/JoinAccept/WithAppSKey/WithQueue",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_JoinAccept{
								JoinAccept: &ttnpb.ApplicationJoinAccept{
									SessionKeyID: "session3",
									AppSKey: &ttnpb.KeyEnvelope{
										// AppSKey is []byte{0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33, 0x33}
										Key:      []byte{0x5, 0x81, 0xe1, 0x15, 0x8a, 0xc3, 0x13, 0x68, 0x5e, 0x8d, 0x15, 0xc0, 0x11, 0x92, 0x14, 0x49, 0x9f, 0xa0, 0xc6, 0xf1, 0xdb, 0x95, 0xff, 0xbd},
										KEKLabel: "test",
									},
									InvalidatedDownlinks: []*ttnpb.ApplicationDownlink{
										{
											SessionKeyID: "session2",
											FPort:        11,
											FCnt:         11,
											FRMPayload:   []byte{0x69, 0x65, 0x9f, 0x8f},
										},
										{
											SessionKeyID: "session2",
											FPort:        22,
											FCnt:         22,
											FRMPayload:   []byte{0xb, 0x8f, 0x94, 0xe6},
										},
									},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_JoinAccept{
									JoinAccept: &ttnpb.ApplicationJoinAccept{
										SessionKeyID: "session3",
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x33, 0x33, 0x33, 0x33},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session3",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x5, 0x81, 0xe1, 0x15, 0x8a, 0xc3, 0x13, 0x68, 0x5e, 0x8d, 0x15, 0xc0, 0x11, 0x92, 0x14, 0x49, 0x9f, 0xa0, 0xc6, 0xf1, 0xdb, 0x95, 0xff, 0xbd},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 2,
								StartedAt:     dev.Session.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(dev.NextSession, should.BeNil)
							a.So(queue, should.Resemble, []*ttnpb.ApplicationDownlink{
								{
									SessionKeyID: "session3",
									FPort:        11,
									FCnt:         1,
									FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
								},
								{
									SessionKeyID: "session3",
									FPort:        22,
									FCnt:         2,
									FRMPayload:   []byte{0x2, 0x2, 0x2, 0x2},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/UplinkMessage/CurrentSession",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_UplinkMessage{
								UplinkMessage: &ttnpb.ApplicationUplink{
									SessionKeyID: "session3",
									FPort:        42,
									FCnt:         42,
									FRMPayload:   []byte{0xca, 0xa9, 0x42},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_UplinkMessage{
									UplinkMessage: &ttnpb.ApplicationUplink{
										SessionKeyID: "session3",
										FPort:        42,
										FCnt:         42,
										FRMPayload:   []byte{0x01, 0x02, 0x03},
										DecodedPayload: &pbtypes.Struct{
											Fields: map[string]*pbtypes.Value{
												"sum": {
													Kind: &pbtypes.Value_NumberValue{
														NumberValue: 6, // Payload formatter sums the bytes in FRMPayload.
													},
												},
											},
										},
									},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkMessage/Queued",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_DownlinkQueued{
								DownlinkQueued: &ttnpb.ApplicationDownlink{
									SessionKeyID: "session3",
									FPort:        42,
									FCnt:         42,
									FRMPayload:   []byte{0x50, 0xd, 0x40, 0xd5},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_DownlinkQueued{
									DownlinkQueued: &ttnpb.ApplicationDownlink{
										SessionKeyID: "session3",
										FPort:        42,
										FCnt:         42,
										FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
									},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkMessage/Sent",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_DownlinkSent{
								DownlinkSent: &ttnpb.ApplicationDownlink{
									SessionKeyID: "session3",
									FPort:        42,
									FCnt:         42,
									FRMPayload:   []byte{0x50, 0xd, 0x40, 0xd5},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_DownlinkSent{
									DownlinkSent: &ttnpb.ApplicationDownlink{
										SessionKeyID: "session3",
										FPort:        42,
										FCnt:         42,
										FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
									},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkMessage/Ack",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_DownlinkAck{
								DownlinkAck: &ttnpb.ApplicationDownlink{
									SessionKeyID: "session3",
									FPort:        42,
									FCnt:         42,
									FRMPayload:   []byte{0x50, 0xd, 0x40, 0xd5},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_DownlinkAck{
									DownlinkAck: &ttnpb.ApplicationDownlink{
										SessionKeyID: "session3",
										FPort:        42,
										FCnt:         42,
										FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
									},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkMessage/Nack",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
							Up: &ttnpb.ApplicationUp_DownlinkNack{
								DownlinkNack: &ttnpb.ApplicationDownlink{
									SessionKeyID: "session3",
									FPort:        42,
									FCnt:         42,
									FRMPayload:   []byte{0x50, 0xd, 0x40, 0xd5},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x33, 0x33, 0x33, 0x33}),
								Up: &ttnpb.ApplicationUp_DownlinkNack{
									DownlinkNack: &ttnpb.ApplicationDownlink{
										SessionKeyID: "session3",
										FPort:        42,
										FCnt:         42,
										FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
									},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/JoinAccept/WithAppSKey/WithQueue/WithNextSession",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
							Up: &ttnpb.ApplicationUp_JoinAccept{
								JoinAccept: &ttnpb.ApplicationJoinAccept{
									SessionKeyID: "session4",
									AppSKey: &ttnpb.KeyEnvelope{
										// AppSKey is []byte{0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44, 0x44}
										Key:      []byte{0x30, 0xcf, 0x47, 0x91, 0x11, 0x64, 0x53, 0x3f, 0xc3, 0xd5, 0xd8, 0x56, 0x5b, 0x71, 0xcb, 0xe7, 0x6d, 0x14, 0x2b, 0x2c, 0xf2, 0xc2, 0xd7, 0x7b},
										KEKLabel: "test",
									},
									NextSession: true,
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
								Up: &ttnpb.ApplicationUp_JoinAccept{
									JoinAccept: &ttnpb.ApplicationJoinAccept{
										SessionKeyID: "session4",
										NextSession:  true,
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x33, 0x33, 0x33, 0x33},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session3",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x5, 0x81, 0xe1, 0x15, 0x8a, 0xc3, 0x13, 0x68, 0x5e, 0x8d, 0x15, 0xc0, 0x11, 0x92, 0x14, 0x49, 0x9f, 0xa0, 0xc6, 0xf1, 0xdb, 0x95, 0xff, 0xbd},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 2,
								StartedAt:     dev.Session.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(dev.NextSession, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x44, 0x44, 0x44, 0x44},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session4",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x30, 0xcf, 0x47, 0x91, 0x11, 0x64, 0x53, 0x3f, 0xc3, 0xd5, 0xd8, 0x56, 0x5b, 0x71, 0xcb, 0xe7, 0x6d, 0x14, 0x2b, 0x2c, 0xf2, 0xc2, 0xd7, 0x7b},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 0,
								StartedAt:     dev.NextSession.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(queue, should.Resemble, []*ttnpb.ApplicationDownlink{
								{
									SessionKeyID: "session3",
									FPort:        11,
									FCnt:         1,
									FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
								},
								{
									SessionKeyID: "session3",
									FPort:        22,
									FCnt:         2,
									FRMPayload:   []byte{0x2, 0x2, 0x2, 0x2},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/UplinkMessage/NextSession",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
							Up: &ttnpb.ApplicationUp_UplinkMessage{
								UplinkMessage: &ttnpb.ApplicationUplink{
									SessionKeyID: "session4",
									FPort:        24,
									FCnt:         24,
									FRMPayload:   []byte{0x14, 0x4e, 0x3c},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
								Up: &ttnpb.ApplicationUp_UplinkMessage{
									UplinkMessage: &ttnpb.ApplicationUplink{
										SessionKeyID: "session4",
										FPort:        24,
										FCnt:         24,
										FRMPayload:   []byte{0x64, 0x64, 0x64},
										DecodedPayload: &pbtypes.Struct{
											Fields: map[string]*pbtypes.Value{
												"sum": {
													Kind: &pbtypes.Value_NumberValue{
														NumberValue: 300, // Payload formatter sums the bytes in FRMPayload.
													},
												},
											},
										},
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x44, 0x44, 0x44, 0x44},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session4",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x30, 0xcf, 0x47, 0x91, 0x11, 0x64, 0x53, 0x3f, 0xc3, 0xd5, 0xd8, 0x56, 0x5b, 0x71, 0xcb, 0xe7, 0x6d, 0x14, 0x2b, 0x2c, 0xf2, 0xc2, 0xd7, 0x7b},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 2,
								StartedAt:     dev.Session.StartedAt,
							})
							a.So(dev.NextSession, should.BeNil)
							a.So(queue, should.Resemble, []*ttnpb.ApplicationDownlink{
								{
									SessionKeyID: "session4",
									FPort:        11,
									FCnt:         1,
									FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
								},
								{
									SessionKeyID: "session4",
									FPort:        22,
									FCnt:         2,
									FRMPayload:   []byte{0x2, 0x2, 0x2, 0x2},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkQueueInvalidated/KnownSession",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
							Up: &ttnpb.ApplicationUp_DownlinkQueueInvalidated{
								DownlinkQueueInvalidated: &ttnpb.ApplicationInvalidatedDownlinks{
									Downlinks: []*ttnpb.ApplicationDownlink{
										{
											SessionKeyID: "session4",
											FPort:        11,
											FCnt:         11,
											FRMPayload:   []byte{0x65, 0x98, 0xa7, 0xfc},
										},
										{
											SessionKeyID: "session4",
											FPort:        22,
											FCnt:         22,
											FRMPayload:   []byte{0x1b, 0x4b, 0x97, 0xb9},
										},
									},
									LastFCntDown: 42,
								},
							},
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session.LastAFCntDown, should.Equal, 44)
							a.So(queue, should.Resemble, []*ttnpb.ApplicationDownlink{
								{
									SessionKeyID: "session4",
									FPort:        11,
									FCnt:         43,
									FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
								},
								{
									SessionKeyID: "session4",
									FPort:        22,
									FCnt:         44,
									FRMPayload:   []byte{0x2, 0x2, 0x2, 0x2},
								},
							})
						},
					},
					{
						Name: "RegisteredDevice/DownlinkQueueInvalidated/UnknownSession",
						IDs:  registeredDevice.EndDeviceIdentifiers,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(registeredDevice.EndDeviceIdentifiers, types.DevAddr{0x44, 0x44, 0x44, 0x44}),
							Up: &ttnpb.ApplicationUp_DownlinkQueueInvalidated{
								DownlinkQueueInvalidated: &ttnpb.ApplicationInvalidatedDownlinks{
									Downlinks: []*ttnpb.ApplicationDownlink{
										{
											SessionKeyID: "session4",
											FPort:        11,
											FCnt:         11,
											FRMPayload:   []byte{0x65, 0x98, 0xa7, 0xfc},
										},
										{
											SessionKeyID: "unknown-session",
											FPort:        12,
											FCnt:         12,
											FRMPayload:   []byte{0xff, 0xff, 0xff, 0xff},
										},
										{
											SessionKeyID: "session4",
											FPort:        22,
											FCnt:         22,
											FRMPayload:   []byte{0x1b, 0x4b, 0x97, 0xb9},
										},
									},
									LastFCntDown: 84,
								},
							},
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session.LastAFCntDown, should.Equal, 86)
							a.So(queue, should.Resemble, []*ttnpb.ApplicationDownlink{
								{
									SessionKeyID: "session4",
									FPort:        11,
									FCnt:         85,
									FRMPayload:   []byte{0x1, 0x1, 0x1, 0x1},
								},
								{
									SessionKeyID: "session4",
									FPort:        22,
									FCnt:         86,
									FRMPayload:   []byte{0x2, 0x2, 0x2, 0x2},
								},
							})
						},
					},
					{
						Name: "UnregisteredDevice/JoinAccept",
						IDs:  unregisteredDeviceID,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(unregisteredDeviceID, types.DevAddr{0x55, 0x55, 0x55, 0x55}),
							Up: &ttnpb.ApplicationUp_JoinAccept{
								JoinAccept: &ttnpb.ApplicationJoinAccept{
									SessionKeyID: "session5",
									AppSKey: &ttnpb.KeyEnvelope{
										// AppSKey is []byte{0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55}
										Key:      []byte{0x56, 0x15, 0xaa, 0x22, 0xb7, 0x5f, 0xc, 0x24, 0x79, 0x6, 0x84, 0x68, 0x89, 0x0, 0xa6, 0x16, 0x4a, 0x9c, 0xef, 0xdb, 0xbf, 0x61, 0x6f, 0x0},
										KEKLabel: "test",
									},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(unregisteredDeviceID, types.DevAddr{0x55, 0x55, 0x55, 0x55}),
								Up: &ttnpb.ApplicationUp_JoinAccept{
									JoinAccept: &ttnpb.ApplicationJoinAccept{
										SessionKeyID: "session5",
									},
								},
							})
						},
						AssertDevice: func(t *testing.T, dev *ttnpb.EndDevice, queue []*ttnpb.ApplicationDownlink) {
							a := assertions.New(t)
							a.So(dev.Session, should.Resemble, &ttnpb.Session{
								DevAddr: types.DevAddr{0x55, 0x55, 0x55, 0x55},
								SessionKeys: ttnpb.SessionKeys{
									SessionKeyID: "session5",
									AppSKey: &ttnpb.KeyEnvelope{
										Key:      []byte{0x56, 0x15, 0xaa, 0x22, 0xb7, 0x5f, 0xc, 0x24, 0x79, 0x6, 0x84, 0x68, 0x89, 0x0, 0xa6, 0x16, 0x4a, 0x9c, 0xef, 0xdb, 0xbf, 0x61, 0x6f, 0x0},
										KEKLabel: "test",
									},
								},
								LastAFCntDown: 0,
								StartedAt:     dev.Session.StartedAt, // TODO: Use join-accept start time (https://github.com/TheThingsIndustries/lorawan-stack/issues/1225)
							})
							a.So(queue, should.HaveLength, 0)
						},
					},
					{
						Name: "UnregisteredDevice/UplinkMessage",
						IDs:  unregisteredDeviceID,
						Message: &ttnpb.ApplicationUp{
							EndDeviceIdentifiers: withDevAddr(unregisteredDeviceID, types.DevAddr{0x55, 0x55, 0x55, 0x55}),
							Up: &ttnpb.ApplicationUp_UplinkMessage{
								UplinkMessage: &ttnpb.ApplicationUplink{
									SessionKeyID: "session5",
									FPort:        11,
									FCnt:         11,
									FRMPayload:   []byte{0xaa, 0x64, 0xb7, 0x7},
								},
							},
						},
						AssertUp: func(t *testing.T, up *ttnpb.ApplicationUp) {
							a := assertions.New(t)
							a.So(up, should.Resemble, &ttnpb.ApplicationUp{
								EndDeviceIdentifiers: withDevAddr(unregisteredDeviceID, types.DevAddr{0x55, 0x55, 0x55, 0x55}),
								Up: &ttnpb.ApplicationUp_UplinkMessage{
									UplinkMessage: &ttnpb.ApplicationUplink{
										SessionKeyID: "session5",
										FPort:        11,
										FCnt:         11,
										FRMPayload:   []byte{0x7, 0x67, 0x0, 0xe1},
										DecodedPayload: &pbtypes.Struct{
											Fields: map[string]*pbtypes.Value{
												"temperature_7": {
													Kind: &pbtypes.Value_NumberValue{
														NumberValue: 22.5, // Application's default formatter is CayenneLPP.
													},
												},
											},
										},
									},
								},
							})
						},
					},
				} {
					tcok := t.Run(tc.Name, func(t *testing.T) {
						ns.upCh <- tc.Message
						select {
						case msg := <-chs.up:
							if tc.AssertUp != nil {
								tc.AssertUp(t, msg)
							} else {
								t.Fatalf("Expected no upstream message but got %v", msg)
							}
						case <-time.After(timeout):
							if tc.AssertUp != nil {
								t.Fatal("Expected upstream timeout")
							}
						}
						if tc.AssertDevice != nil {
							dev, err := deviceRegistry.Get(ctx, tc.Message.EndDeviceIdentifiers, []string{"session", "next_session"})
							if !a.So(err, should.BeNil) {
								t.FailNow()
							}
							queue, err := as.DownlinkQueueList(ctx, tc.IDs)
							if !a.So(err, should.BeNil) {
								t.FailNow()
							}
							tc.AssertDevice(t, dev, queue)
						}
					})
					if !tcok {
						t.FailNow()
					}
				}
			})

			t.Run("Downstream", func(t *testing.T) {
				ns.reset()
				deviceRegistry.Reset()
				deviceRegistry.Set(ctx, registeredDevice.EndDeviceIdentifiers, nil, func(_ *ttnpb.EndDevice) (*ttnpb.EndDevice, []string, error) {
					dev := *registeredDevice
					dev.Session = &ttnpb.Session{
						DevAddr: types.DevAddr{0x42, 0xff, 0xff, 0xff},
						SessionKeys: ttnpb.SessionKeys{
							SessionKeyID: "session1",
							AppSKey: &ttnpb.KeyEnvelope{
								Key:      []byte{0x1f, 0xa6, 0x8b, 0xa, 0x81, 0x12, 0xb4, 0x47, 0xae, 0xf3, 0x4b, 0xd8, 0xfb, 0x5a, 0x7b, 0x82, 0x9d, 0x3e, 0x86, 0x23, 0x71, 0xd2, 0xcf, 0xe5},
								KEKLabel: "test",
							},
						},
					}
					return &dev, []string{"ids", "version_ids", "session", "formatters"}, nil
				})
				t.Run("UnregisteredDevice/Push", func(t *testing.T) {
					a := assertions.New(t)
					chs.downPush <- &ttnpb.DownlinkQueueRequest{
						EndDeviceIdentifiers: unregisteredDeviceID,
						Downlinks: []*ttnpb.ApplicationDownlink{
							{
								FPort:      11,
								FRMPayload: []byte{0x1, 0x1, 0x1},
							},
						},
					}
					select {
					case err := <-chs.downErr:
						if a.So(err, should.NotBeNil) {
							a.So(errors.IsNotFound(err), should.BeTrue)
						}
					case <-time.After(timeout):
						t.Fatal("Expected downlink error timeout")
					}
				})
				t.Run("RegisteredDevice/Push", func(t *testing.T) {
					a := assertions.New(t)
					for _, items := range [][]*ttnpb.ApplicationDownlink{
						{
							{
								FPort:      11,
								FRMPayload: []byte{0x1, 0x1, 0x1},
							},
							{
								FPort:      22,
								FRMPayload: []byte{0x2, 0x2, 0x2},
							},
						},
						{
							{
								FPort: 33,
								DecodedPayload: &pbtypes.Struct{
									Fields: map[string]*pbtypes.Value{
										"sum": {
											Kind: &pbtypes.Value_NumberValue{
												NumberValue: 6, // Payload formatter returns a byte slice with this many 1s.
											},
										},
									},
								},
							},
						},
					} {
						chs.downPush <- &ttnpb.DownlinkQueueRequest{
							EndDeviceIdentifiers: registeredDevice.EndDeviceIdentifiers,
							Downlinks:            items,
						}
						select {
						case err := <-chs.downErr:
							if !a.So(err, should.BeNil) {
								t.FailNow()
							}
						case <-time.After(timeout):
							t.Fatal("Expected downlink error timeout")
						}
					}
					res, err := as.DownlinkQueueList(ctx, registeredDevice.EndDeviceIdentifiers)
					a.So(err, should.BeNil)
					a.So(res, should.HaveLength, 3)
					a.So(res, should.Resemble, []*ttnpb.ApplicationDownlink{
						{
							SessionKeyID:   "session1",
							FPort:          11,
							FCnt:           1,
							FRMPayload:     []byte{0x1, 0x1, 0x1},
							CorrelationIDs: res[0].CorrelationIDs,
						},
						{
							SessionKeyID:   "session1",
							FPort:          22,
							FCnt:           2,
							FRMPayload:     []byte{0x2, 0x2, 0x2},
							CorrelationIDs: res[1].CorrelationIDs,
						},
						{
							SessionKeyID:   "session1",
							FPort:          33,
							FCnt:           3,
							FRMPayload:     []byte{0x1, 0x1, 0x1, 0x1, 0x1, 0x1},
							CorrelationIDs: res[2].CorrelationIDs,
						},
					})
				})
				t.Run("RegisteredDevice/Replace", func(t *testing.T) {
					a := assertions.New(t)
					chs.downReplace <- &ttnpb.DownlinkQueueRequest{
						EndDeviceIdentifiers: registeredDevice.EndDeviceIdentifiers,
						Downlinks: []*ttnpb.ApplicationDownlink{
							{
								FPort:      11,
								FRMPayload: []byte{0x1, 0x1, 0x1},
							},
							{
								FPort:      22,
								FRMPayload: []byte{0x2, 0x2, 0x2},
							},
						},
					}
					select {
					case err := <-chs.downErr:
						if !a.So(err, should.BeNil) {
							t.FailNow()
						}
					case <-time.After(timeout):
						t.Fatal("Expected downlink error timeout")
					}
					res, err := as.DownlinkQueueList(ctx, registeredDevice.EndDeviceIdentifiers)
					a.So(err, should.BeNil)
					a.So(res, should.HaveLength, 2)
					a.So(res, should.Resemble, []*ttnpb.ApplicationDownlink{
						{
							SessionKeyID:   "session1",
							FPort:          11,
							FCnt:           4,
							FRMPayload:     []byte{0x1, 0x1, 0x1},
							CorrelationIDs: res[0].CorrelationIDs,
						},
						{
							SessionKeyID:   "session1",
							FPort:          22,
							FCnt:           5,
							FRMPayload:     []byte{0x2, 0x2, 0x2},
							CorrelationIDs: res[1].CorrelationIDs,
						},
					})
				})
			})

			cancel()
			wg.Wait()
		})
	}
}