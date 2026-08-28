package httpapi

func (s *Server) RegisterJobs() {
	if s.Jobs == nil {
		return
	}
	s.Jobs.Register("library.merge", s.jobLibraryMerge)
	s.Jobs.Register("library.delete", s.jobLibraryDelete)
	s.Jobs.Register("library.cleanup_files", s.jobLibraryCleanup)
	s.Jobs.Register("tracks.bulk_delete", s.jobTracksDelete)
	s.Jobs.Register("tracks.metadata", s.jobTracksMetadata)
	s.Jobs.Register("scapex.fetch", s.jobScapeXFetch)
	s.RegisterStatsJobs()
	s.RegisterReplaceJobs()
}
