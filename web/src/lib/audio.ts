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
