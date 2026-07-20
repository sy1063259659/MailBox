const blockTagPattern = /<\/?(?:address|article|aside|blockquote|div|footer|h[1-6]|header|li|main|ol|p|pre|section|table|tbody|td|th|thead|tr|ul)\b[^>]*>/gi
const breakTagPattern = /<br\s*\/?\s*>/gi
const remainingTagPattern = /<[^>]+>/g
const htmlCommentPattern = /<!--[\s\S]*?-->/g
const hiddenHtmlPattern = /<(?:head|script|style|title)\b[^>]*>[\s\S]*?<\/(?:head|script|style|title)>/gi

function decodeQuotedPrintable(value: string): string {
  if (!/=([\dA-F]{2})/i.test(value)) {
    return value
  }
  const source = value.replace(/=\r?\n/g, '')

  const bytes: number[] = []
  const encoder = new TextEncoder()
  for (let index = 0; index < source.length;) {
    const encodedByte = source.slice(index).match(/^=([\dA-F]{2})/i)
    if (encodedByte) {
      bytes.push(Number.parseInt(encodedByte[1], 16))
      index += 3
      continue
    }

    const codePoint = source.codePointAt(index)
    if (codePoint === undefined) {
      break
    }
    const character = String.fromCodePoint(codePoint)
    bytes.push(...encoder.encode(character))
    index += character.length
  }
  return new TextDecoder('utf-8').decode(Uint8Array.from(bytes))
}

function decodeHtmlEntities(value: string): string {
  const namedEntities: Record<string, string> = {
    amp: '&',
    apos: "'",
    gt: '>',
    lt: '<',
    nbsp: ' ',
    quot: '"',
  }
  return value.replace(/&(#x[\dA-F]+|#\d+|amp|apos|gt|lt|nbsp|quot);/gi, (entity, code: string) => {
    if (code.startsWith('#x') || code.startsWith('#X')) {
      return String.fromCodePoint(Number.parseInt(code.slice(2), 16))
    }
    if (code.startsWith('#')) {
      return String.fromCodePoint(Number.parseInt(code.slice(1), 10))
    }
    return namedEntities[code.toLowerCase()] ?? entity
  })
}

export function plainMailParagraphs(value: string): string[] {
  const readableText = decodeHtmlEntities(decodeQuotedPrintable(value))
    .replace(hiddenHtmlPattern, '')
    .replace(htmlCommentPattern, '')
    .replace(breakTagPattern, '\n')
    .replace(blockTagPattern, '\n')
    .replace(remainingTagPattern, ' ')
    .replace(/\r\n?/g, '\n')
    .replace(/[\u00a0\u200b\ufeff]/g, ' ')

  const paragraphs: string[] = []
  let currentLines: string[] = []
  const flush = () => {
    const paragraph = currentLines.join(' ').replace(/[ \t]+/g, ' ').trim()
    if (paragraph) {
      paragraphs.push(paragraph)
    }
    currentLines = []
  }

  for (const rawLine of readableText.split('\n')) {
    const line = rawLine.replace(/[ \t]+/g, ' ').trim()
    if (!line) {
      flush()
      continue
    }
    currentLines.push(line)
  }
  flush()

  return paragraphs
}
