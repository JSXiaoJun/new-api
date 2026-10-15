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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'

import type { TaskRequestSnapshot } from '../types'

function RequestSection(props: { title: string; children: React.ReactNode }) {
  return (
    <section className='flex flex-col gap-2'>
      <h3 className='text-sm font-semibold'>{props.title}</h3>
      {props.children}
    </section>
  )
}

function NotRecorded() {
  const { t } = useTranslation()
  return (
    <p className='text-muted-foreground text-sm'>
      {t('Not recorded for this task')}
    </p>
  )
}

function OriginalLink(props: { href: string }) {
  const { t } = useTranslation()
  return (
    <a
      href={props.href}
      target='_blank'
      rel='noopener noreferrer'
      className='text-xs hover:underline'
    >
      {t('View original')}
    </a>
  )
}

export function TaskRequestDetails(props: { request: TaskRequestSnapshot }) {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-4 border-t pt-4'>
      <RequestSection title={t('Reference images')}>
        {props.request.images.length > 0 ? (
          <ul className='flex flex-wrap gap-3'>
            {props.request.images.map((url, index) => (
              <li key={url} className='flex w-36 flex-col gap-1'>
                <a
                  href={url}
                  target='_blank'
                  rel='noopener noreferrer'
                  className='bg-muted/40 block h-28 overflow-hidden rounded-md border'
                >
                  <img
                    src={url}
                    alt={t('Reference image {{index}}', { index: index + 1 })}
                    loading='lazy'
                    className='size-full object-contain'
                  />
                </a>
                <OriginalLink href={url} />
              </li>
            ))}
          </ul>
        ) : (
          <NotRecorded />
        )}
      </RequestSection>

      <RequestSection title={t('Reference videos')}>
        {props.request.videos.length > 0 ? (
          <ul className='grid gap-3 sm:grid-cols-2'>
            {props.request.videos.map((url) => (
              <li key={url} className='flex flex-col gap-1'>
                <video
                  src={url}
                  controls
                  preload='metadata'
                  className='max-h-60 w-full rounded-md bg-black'
                />
                <OriginalLink href={url} />
              </li>
            ))}
          </ul>
        ) : (
          <NotRecorded />
        )}
      </RequestSection>

      {props.request.audios.length > 0 ? (
        <RequestSection title={t('Reference audios')}>
          <ul className='flex flex-col gap-2'>
            {props.request.audios.map((url) => (
              <li key={url}>
                <audio src={url} controls preload='none' className='w-full' />
              </li>
            ))}
          </ul>
        </RequestSection>
      ) : null}

      <RequestSection title={t('Submitted prompt')}>
        {props.request.prompt ? (
          <div className='flex items-start gap-2'>
            <p className='min-w-0 flex-1 text-sm leading-relaxed break-words whitespace-pre-wrap'>
              {props.request.prompt}
            </p>
            <CopyButton
              value={props.request.prompt}
              size='icon'
              className='size-7 shrink-0'
              tooltip={t('Copy prompt')}
            />
          </div>
        ) : (
          <NotRecorded />
        )}
      </RequestSection>
    </div>
  )
}
