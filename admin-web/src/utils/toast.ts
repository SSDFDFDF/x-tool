import { hideMessage, messageState, showMessage, type MessageType } from './message'

export type ToastType = MessageType

export const toastState = messageState
export const hideToast = hideMessage
export const showToast = showMessage
export const showSuccessToast = showMessage.success
export const showErrorToast = showMessage.error
export const showWarningToast = showMessage.warning
