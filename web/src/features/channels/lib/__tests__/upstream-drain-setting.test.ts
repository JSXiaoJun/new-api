/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import { CHANNEL_TYPE_SUB2API } from '../../constants'
import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  transformChannelToFormDefaults,
} from '../channel-form'

function sub2apiChannel(setting: Record<string, unknown>) {
  return channelSchema.parse({
    id: 7,
    type: CHANNEL_TYPE_SUB2API,
    key: '',
    status: 1,
    name: 'sub2api',
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-5',
    group: 'default',
    used_quota: 0,
    setting: JSON.stringify(setting),
  })
}

describe('upstream drain channel setting', () => {
  test('an untouched channel keeps following the channel type default', () => {
    const values = transformChannelToFormDefaults(sub2apiChannel({}))

    expect(values.upstream_drain_on_disconnect).toBeUndefined()
    expect(JSON.parse(buildSettingJSON(values))).not.toHaveProperty(
      'upstream_drain_on_disconnect'
    )
  })

  test.each([true, false])('an explicit %s survives a save', (enabled) => {
    const values = transformChannelToFormDefaults(
      sub2apiChannel({ upstream_drain_on_disconnect: enabled })
    )

    expect(values.upstream_drain_on_disconnect).toBe(enabled)
    expect(JSON.parse(buildSettingJSON(values))).toMatchObject({
      upstream_drain_on_disconnect: enabled,
    })
  })

  test('a new channel leaves the setting to the type default', () => {
    expect(
      JSON.parse(
        buildSettingJSON({
          ...CHANNEL_FORM_DEFAULT_VALUES,
          type: CHANNEL_TYPE_SUB2API,
        })
      )
    ).not.toHaveProperty('upstream_drain_on_disconnect')
  })
})
