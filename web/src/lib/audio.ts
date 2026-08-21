export const AUDIO_EXTS = [".mp3", ".flac", ".aac", ".m4a", ".alac", ".ogg", ".opus", ".wav", ".oga", ".aif", ".aiff"] as const;

export const AUDIO_ACCEPT = [
  ...AUDIO_EXTS,
  "audio/mpeg",
  "audio/flac",
  "audio/mp4",
  "audio/aac",
  "audio/ogg",
  "audio/wav",
  "audio/x-wav",
  "audio/aiff",
  "audio/*"
].join(",");

export function isAudioFile(file: File) {
  const name = file.name.toLowerCase();
  if (AUDIO_EXTS.some((ext) => name.endsWith(ext))) return true;
  return file.type.startsWith("audio/");
}

export function isZipFile(file: File) {
  const name = file.name.toLowerCase();
  return name.endsWith(".zip") || file.type === "application/zip" || file.type === "application/x-zip-compressed";
}

export function isBulkUploadFile(file: File) {
  return isAudioFile(file) || isZipFile(file);
}

export const UPLOAD_ACCEPT = `${AUDIO_ACCEPT},.zip,application/zip,application/x-zip-compressed`;

