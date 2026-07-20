package ai

// ImageStyleCatalog is intentionally keyed by stable identifiers stored in
// published template configuration. The instructions are expanded only while
// compiling a provider request so UI labels never become prompt semantics.
var ImageStyleCatalog = map[string]string{
	"clean_white_background": "Photorealistic ecommerce studio photograph on a seamless pure-white background, centered product, soft even lighting, crisp edges, and a subtle grounding shadow.",
	"soft_studio":            "Photorealistic studio photograph on a quiet neutral gradient, large diffused softboxes, gentle dimensional shadows, balanced centered composition, and restrained commercial polish.",
	"high_key_airy":          "High-key photorealistic product photograph with an off-white airy background, bright diffuse illumination, low visual clutter, soft shadows, and generous negative space.",
	"warm_neutral":           "Photorealistic product photograph in a warm beige and taupe studio setting, soft warm diffused light, natural material accents, calm composition, and restrained shadows.",
	"premium_dark":           "Premium photorealistic product photograph on charcoal or black, controlled rim and key lighting, deep but readable shadows, precise highlights, and a product-dominant composition.",
	"luxury_editorial":       "Luxury editorial product photograph with refined surfaces, dramatic but accurate studio lighting, intentional negative space, restrained premium styling, and magazine-grade composition.",
	"minimal_gradient":       "Clean commercial product photograph against a smooth minimal color gradient, soft studio lighting, simple centered geometry, ample negative space, and no decorative clutter.",
	"bold_color_block":       "Commercial product photograph with saturated geometric color-blocked surfaces, crisp directional lighting, bold negative-space composition, and the product clearly dominant.",
	"vibrant_pop":            "Energetic photorealistic advertising image with bright harmonious colors, playful restrained props, lively studio lighting, clean separation, and a product-first composition.",
	"pastel_soft":            "Soft pastel commercial product photograph with muted harmonious colors, gentle diffuse lighting, rounded shadows, uncluttered styling, and a calm product-focused composition.",
	"natural_daylight":       "Realistic lifestyle product photograph in a relevant everyday setting, natural window daylight, believable materials and shadows, uncluttered context, and no implied unsupported capability.",
	"cozy_home":              "Realistic warm home lifestyle photograph, soft ambient and window light, tidy relatable surroundings, natural textures, comfortable mood, and the exact product remaining prominent.",
	"modern_urban":           "Realistic modern urban lifestyle photograph using restrained concrete, glass, or metal context, cool natural daylight, clean architectural lines, and a product-dominant frame.",
	"outdoor_active":         "Realistic outdoor lifestyle photograph with natural sunlight, believable environmental context, energetic composition, and no visual implication of unsupported durability or performance.",
	"flat_lay":               "Editorial top-down flat-lay photograph, carefully spaced arrangement, clean background, soft directional light, controlled shadows, and only relevant non-misleading supporting objects.",
	"macro_material":         "Photorealistic macro detail photograph emphasizing visible material, texture, finish, or construction, precise focus, shallow depth of field, controlled highlights, and faithful geometry.",
	"technical_3d":           "Precise photorealistic technical 3D product render with physically plausible materials, clean neutral environment, controlled studio lighting, accurate geometry, and no invented internal parts.",
	"isometric_illustration": "Clean commercial isometric illustration derived from the exact product geometry and colors, simple background, consistent soft lighting, restrained detail, and no invented components.",
	"clean_infographic":      "Clean ecommerce infographic layout with a product-dominant image, structured fact callouts, generous text-safe space, clear hierarchy, and only explicitly supported visible claims.",
	"seasonal_campaign":      "Photorealistic seasonal campaign image with restrained culturally neutral seasonal accents, polished commercial lighting, uncluttered composition, and the exact product remaining dominant.",
}

func imageStyleInstruction(style string) string {
	if instruction, ok := ImageStyleCatalog[style]; ok {
		return instruction
	}
	return style
}
