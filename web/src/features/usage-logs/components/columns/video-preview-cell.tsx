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
import { Play } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

export function VideoPreviewCell({ taskId }: { taskId: string }) {
  const { t } = useTranslation()
  const videoUrl = `/public/videos/${encodeURIComponent(taskId)}/content`

  return (
    <Button
      render={<a href={videoUrl} target='_blank' rel='noopener noreferrer' />}
      variant='ghost'
      size='xs'
      className='text-foreground h-6 px-1.5 text-xs'
    >
      <Play className='size-3' />
      {t('Click to preview video')}
    </Button>
  )
}
