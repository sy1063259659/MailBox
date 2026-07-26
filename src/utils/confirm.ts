import { ElMessageBox } from 'element-plus'
import type { ElMessageBoxOptions } from 'element-plus'

/**
 * ElMessageBox rejects when the user cancels or presses Esc, which turns every
 * plain `await ElMessageBox.confirm(...)` into an unhandled promise rejection.
 * These helpers translate a cancellation into a normal return value so callers
 * can branch on it instead of relying on an exception.
 */

export async function confirmAction(
  message: string,
  title: string,
  options: ElMessageBoxOptions = { type: 'warning' },
): Promise<boolean> {
  try {
    await ElMessageBox.confirm(message, title, options)
    return true
  } catch {
    return false
  }
}

/** Returns the entered text, or undefined when the prompt was cancelled. */
export async function promptForValue(
  message: string,
  title: string,
  options: ElMessageBoxOptions = {},
): Promise<string | undefined> {
  try {
    const result = await ElMessageBox.prompt(message, title, options)
    return result.value ?? ''
  } catch {
    return undefined
  }
}
