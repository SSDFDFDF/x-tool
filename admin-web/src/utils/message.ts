import { ref } from 'vue'

export type MessageType = 'success' | 'error' | 'info' | 'warning'

type ShowMessageOptions = {
  type?: MessageType
  title?: string
  message: string
  duration?: number
}

const DEFAULT_DURATION = 4000

let hideTimer: ReturnType<typeof setTimeout> | null = null

export const messageState = {
  visible: ref(false),
  type: ref<MessageType>('info'),
  title: ref(''),
  message: ref('')
}

export const hideMessage = () => {
  messageState.visible.value = false

  if (hideTimer) {
    clearTimeout(hideTimer)
    hideTimer = null
  }
}

const openMessage = ({ type = 'info', title = '', message, duration = DEFAULT_DURATION }: ShowMessageOptions) => {
  if (hideTimer) {
    clearTimeout(hideTimer)
    hideTimer = null
  }

  messageState.type.value = type
  messageState.title.value = title
  messageState.message.value = message
  messageState.visible.value = true

  if (duration > 0) {
    hideTimer = setTimeout(() => {
      hideMessage()
    }, duration)
  }
}

type ShowMessageFn = ((options: ShowMessageOptions) => void) & {
  success: (message: string, title?: string) => void
  error: (message: string, title?: string) => void
  warning: (message: string, title?: string) => void
  info: (message: string, title?: string) => void
  close: () => void
}

export const showMessage: ShowMessageFn = Object.assign(
  (options: ShowMessageOptions) => {
    openMessage(options)
  },
  {
    success: (message: string, title = '操作成功') => {
      openMessage({ type: 'success', title, message })
    },
    error: (message: string, title = '操作失败') => {
      openMessage({ type: 'error', title, message, duration: 5000 })
    },
    warning: (message: string, title = '请检查输入') => {
      openMessage({ type: 'warning', title, message })
    },
    info: (message: string, title = '提示') => {
      openMessage({ type: 'info', title, message })
    },
    close: () => {
      hideMessage()
    }
  }
)
