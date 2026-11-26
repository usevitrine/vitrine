package media

// Rating is the content rating a site gives a post. It is a field, not a tag.
// The numeric values are stored, so they must never change.
type Rating uint8

const (
	RatingUnknown Rating = iota
	RatingGeneral        // "safe" on many sites
	RatingSensitive
	RatingQuestionable
	RatingExplicit
)

var ratingNames = [...]string{"unknown", "general", "sensitive", "questionable", "explicit"}

func (r Rating) String() string {
	if int(r) < len(ratingNames) {
		return ratingNames[r]
	}
	return ratingNames[0]
}

// ParseRating reads a rating name. "safe" is accepted for general. Single
// letters are not, because sites disagree on them (on some sites "s" is safe,
// Danbooru's is sensitive) - plugins map their site's values themselves.
func ParseRating(s string) (Rating, bool) {
	if s == "safe" {
		return RatingGeneral, true
	}
	for i, n := range ratingNames {
		if s == n {
			return Rating(i), true
		}
	}
	return RatingUnknown, false
}
