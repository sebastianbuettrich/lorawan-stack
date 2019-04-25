// Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
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

import React, { Component } from 'react'
import { Container, Col, Row } from 'react-grid-system'
import { connect } from 'react-redux'
import bind from 'autobind-decorator'
import { push } from 'connected-react-router'

import Form from '../../../components/form'
import Field from '../../../components/field'
import Button from '../../../components/button'
import Breadcrumb from '../../../components/breadcrumbs/breadcrumb'
import { withBreadcrumb } from '../../../components/breadcrumbs/context'
import FieldGroup from '../../../components/field/group'
import Message from '../../../lib/components/message'
import IntlHelmet from '../../../lib/components/intl-helmet'
import SubmitBar from '../../../components/submit-bar'

import api from '../../api'
import sharedMessages from '../../../lib/shared-messages'
import m from './messages'
import validationSchema from './validation-schema'
import style from './device-add.styl'

@withBreadcrumb('device.add', function (props) {
  const { appId } = props.match.params
  return (
    <Breadcrumb
      path={`/console/applications/${appId}/devices/add`}
      icon="add"
      content={sharedMessages.add}
    />
  )
})
@connect(({ user }) => ({
  userId: user.user.ids.user_id,
}))
@bind
export default class DeviceAdd extends Component {
  state = {
    error: '',
    otaa: true,
  }

  async handleSubmit (values, { setSubmitting, resetForm }) {
    const { match, dispatch } = this.props
    const { appId } = match.params

    const device = {
      ...values,
      ids: {
        ...values.ids,
      },
      frequency_plan_id: 'EU_863_870',
    }
    delete device.activation_mode

    await this.setState({ error: '' })
    try {
      const result = await api.devices.create(appId, device, {
        abp: values.activation_mode === 'abp',
        withRootKeys: true,
      })

      const { ids: { device_id }} = result
      dispatch(push(`/console/applications/${appId}/devices/${device_id}`))
    } catch (error) {
      resetForm(values)

      await this.setState({ error })
    }
  }

  handleOTAASelect () {
    this.setState({ otaa: true })
  }

  handleABPSelect () {
    this.setState({ otaa: false })
  }

  ABPSection (props) {
    return (
      <React.Fragment>
        <Field
          title={sharedMessages.devAddr}
          name="session.dev_addr"
          type="byte"
          min={4}
          max={4}
          placeholder={m.leaveBlankPlaceholder}
          description={m.deviceAddrDescription}
        />
        <Field
          title={sharedMessages.fwdNtwkKey}
          name="session.keys.f_nwk_s_int_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.fwdNtwkKeyDescription}
        />
        <Field
          title={sharedMessages.sNtwkSIKey}
          name="session.keys.s_nwk_s_int_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.sNtwkSIKeyDescription}
        />
        <Field
          title={sharedMessages.ntwkSEncKey}
          name="session.keys.nwk_s_enc_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.ntwkSEncKeyDescription}
        />
        <Field
          title={sharedMessages.appSKey}
          name="session.keys.app_s_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.appSKeyDescription}
        />
      </React.Fragment>
    )
  }

  OTAASection () {
    return (
      <React.Fragment>
        <Field
          title={sharedMessages.joinEUI}
          name="ids.join_eui"
          type="byte"
          min={8}
          max={8}
          placeholder={m.joinEUIPlaceholder}
          required
        />
        <Field
          title={sharedMessages.devEUI}
          name="ids.dev_eui"
          type="byte"
          min={8}
          max={8}
          placeholder={m.deviceEUIPlaceholder}
          description={m.deviceEUIDescription}
          required
        />
        <Field
          title={sharedMessages.nwkKey}
          name="root_keys.nwk_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.nwkKeyDescription}

        />
        <Field
          title={sharedMessages.appKey}
          name="root_keys.app_key.key"
          type="byte"
          min={16}
          max={16}
          placeholder={m.leaveBlankPlaceholder}
          description={m.appKeyDescription}
        />
        <Field
          title={m.resetsJoinNonces}
          name="resets_join_nonces"
          type="checkbox"
        />
      </React.Fragment>
    )
  }

  render () {
    const { error, otaa } = this.state

    return (
      <Container>
        <Row className={style.wrapper}>
          <Col sm={12}>
            <IntlHelmet title={sharedMessages.addDevice} />
            <Message component="h2" content={sharedMessages.addDevice} />
          </Col>
          <Col className={style.form} sm={12} md={12} lg={8} xl={8}>
            <Form
              error={error}
              onSubmit={this.handleSubmit}
              validationSchema={validationSchema}
              submitEnabledWhenInvalid
              isInitialValid={false}
              initialValues={
                {
                  ids: {
                    device_id: undefined,
                    join_eui: undefined,
                    dev_eui: undefined,
                  },
                  activation_mode: 'otaa',
                  lorawan_version: undefined,
                  lorawan_phy_version: undefined,
                  root_keys: {},
                  session: {},
                }
              }
              mapErrorsToFields={{
                id_taken: 'application_id',
                identifiers: 'application_id',
              }}
              horizontal
            >
              <Message
                component="h4"
                content={sharedMessages.generalSettings}
              />
              <Field
                title={sharedMessages.devID}
                name="ids.device_id"
                placeholder={m.deviceIdPlaceholder}
                description={m.deviceIdDescription}
                autoFocus
                required
              />
              <Field
                title={sharedMessages.devName}
                name="name"
                placeholder={m.deviceNamePlaceholder}
                description={m.deviceNameDescription}
              />
              <Field
                title={sharedMessages.devDesc}
                name="description"
                type="textarea"
                description={m.deviceDescDescription}
              />
              <Message
                component="h4"
                content={m.lorawanOptions}
              />
              <Field
                title={sharedMessages.macVersion}
                name="lorawan_version"
                type="select"
                required
                options={[
                  { value: 'MAC_V1_0', label: 'MAC V1.0' },
                  { value: 'MAC_V1_0_1', label: 'MAC V1.0.1' },
                  { value: 'MAC_V1_0_2', label: 'MAC V1.0.2' },
                  { value: 'MAC_V1_1', label: 'MAC V1.1' },
                ]}
              />
              <Field
                title={sharedMessages.phyVersion}
                name="lorawan_phy_version"
                type="select"
                required
                options={[
                  { value: 'PHY_V1_0', label: 'PHY V1.0' },
                  { value: 'PHY_V1_0_1', label: 'PHY V1.0.1' },
                  { value: 'PHY_V1_0_2_REV_A', label: 'PHY V1.0.2 REV A' },
                  { value: 'PHY_V1_0_2_REV_B', label: 'PHY V1.0.2 REV B' },
                  { value: 'PHY_V1_1_REV_A', label: 'PHY V1.1 REV A' },
                  { value: 'PHY_V1_1_REV_B', label: 'PHY V1.1 REV B' },
                ]}
              />
              <Field
                title="Supports Class C"
                name="supports_class_c"
                type="checkbox"
              />
              <Message
                component="h4"
                content={m.activationSettings}
              />
              <FieldGroup
                name="activation_mode"
                title="Activation Mode"
                columns
              >
                <Field
                  title="Over The Air Activation (OTAA)"
                  value="otaa"
                  type="radio"
                  onChange={this.handleOTAASelect}
                />
                <Field
                  title="Activation By Personalization (ABP)"
                  value="abp"
                  type="radio"
                  onChange={this.handleABPSelect}
                />
              </FieldGroup>
              {otaa ? this.OTAASection() : this.ABPSection()}
              <SubmitBar>
                <Button type="submit" message={m.createDevice} />
              </SubmitBar>
            </Form>
          </Col>
        </Row>
      </Container>
    )
  }
}
