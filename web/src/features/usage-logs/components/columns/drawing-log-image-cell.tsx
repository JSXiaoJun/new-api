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
import { ImageIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import { ImageDialog } from '../dialogs/image-dialog'

interface DrawingLogImageCellProps {
  imageUrls: string[]
  label: string
}

/**
 * Preview control for the generated images of one drawing log row.
 *
 * A single image opens the preview dialog directly. Several images open a list
 * first, because one dialog cannot show them all without hiding which image is
 * which. Every link opens in a new tab as well, so the image can be looked at
 * or downloaded without this dashboard.
 */
export function DrawingLogImageCell({
  imageUrls,
  label,
}: DrawingLogImageCellProps) {
  const { t } = useTranslation()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [listOpen, setListOpen] = useState(false)

  if (imageUrls.length === 0) {
    return <span className='text-muted-foreground/60 text-xs'>-</span>
  }

  if (imageUrls.length === 1) {
    return (
      <>
        <button
          type='button'
          className='group text-left text-xs'
          onClick={() => setDialogOpen(true)}
          title={t('Click to view image')}
        >
          <span className='text-foreground truncate leading-snug group-hover:underline'>
            {label}
          </span>
        </button>
        <ImageDialog
          imageUrl={imageUrls[0]}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
        />
      </>
    )
  }

  return (
    <Popover open={listOpen} onOpenChange={setListOpen}>
      <PopoverTrigger
        render={
          <Button
            variant='ghost'
            size='xs'
            className='text-foreground h-6 gap-1 px-1.5 text-xs'
          />
        }
      >
        <ImageIcon className='size-3' />
        {t('{{count}} images', { count: imageUrls.length })}
      </PopoverTrigger>
      <PopoverContent align='start' className='w-72 p-2'>
        <div className='flex flex-col gap-1'>
          {imageUrls.map((imageUrl, index) => (
            <a
              key={imageUrl}
              href={imageUrl}
              target='_blank'
              rel='noopener noreferrer'
              className='hover:bg-muted/60 truncate rounded-md px-2 py-1.5 text-xs'
              title={imageUrl}
            >
              {t('Image {{index}}', { index: index + 1 })}
            </a>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  )
}
