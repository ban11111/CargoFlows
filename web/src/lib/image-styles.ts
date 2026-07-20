export type ImageStylePreset = {
  key: string;
  name: { zh: string; en: string };
  description: { zh: string; en: string };
};

export const imageStylePresets: readonly ImageStylePreset[] = [
  ["clean_white_background", "白底棚拍", "White-background studio", "纯白背景、柔和落地阴影，适合平台主图。", "Pure white, soft grounding shadow, ideal for hero images."],
  ["soft_studio", "柔光影棚", "Soft studio", "中性渐变背景与柔光箱，立体但克制。", "Neutral gradient and diffused softboxes with restrained depth."],
  ["high_key_airy", "明亮通透", "High-key airy", "高调柔光、低杂讯与充足留白。", "Bright diffuse light, low clutter, and generous negative space."],
  ["warm_neutral", "暖调中性", "Warm neutral", "米色暖调、自然材质与柔和阴影。", "Warm beige tones, natural materials, and soft shadows."],
  ["premium_dark", "高级暗调", "Premium dark", "深色背景、轮廓光与精准高光。", "Dark backdrop, rim lighting, and precise highlights."],
  ["luxury_editorial", "奢华杂志", "Luxury editorial", "精致表面、戏剧化光线和杂志式留白。", "Refined surfaces, dramatic light, and editorial negative space."],
  ["minimal_gradient", "极简渐变", "Minimal gradient", "平滑渐变背景与简洁居中构图。", "Smooth gradient background and minimal centered composition."],
  ["bold_color_block", "大胆色块", "Bold color block", "饱和几何色块与清晰方向光。", "Saturated geometric blocks and crisp directional light."],
  ["vibrant_pop", "缤纷潮流", "Vibrant pop", "明快配色、少量趣味道具与活力光线。", "Bright colors, restrained playful props, and lively light."],
  ["pastel_soft", "柔和粉彩", "Pastel soft", "低饱和粉彩、柔光与圆润阴影。", "Muted pastels, gentle light, and rounded shadows."],
  ["natural_daylight", "自然日光场景", "Natural daylight", "真实日常环境与自然窗光。", "Real everyday context with natural window light."],
  ["cozy_home", "温馨居家", "Cozy home", "整洁居家环境、暖光与自然纹理。", "Tidy home context, warm light, and natural textures."],
  ["modern_urban", "现代都市", "Modern urban", "混凝土、玻璃或金属环境与冷调日光。", "Concrete, glass, or metal context with cool daylight."],
  ["outdoor_active", "户外活力", "Outdoor active", "真实户外光线与有活力但不夸大的场景。", "Real outdoor light and energetic context without overclaiming."],
  ["flat_lay", "编辑式平铺", "Editorial flat lay", "俯拍、整齐间距与相关辅助物件。", "Top-down spacing with relevant supporting objects."],
  ["macro_material", "材质微距", "Material macro", "突出可见材质、纹理、表面和做工。", "Highlights visible material, texture, finish, and construction."],
  ["technical_3d", "技术型 3D 渲染", "Technical 3D", "物理可信材质与精确商品几何。", "Physically plausible materials and precise product geometry."],
  ["isometric_illustration", "等距商业插画", "Isometric illustration", "基于真实外形和配色的简洁等距插画。", "Clean isometric illustration based on exact shape and colors."],
  ["clean_infographic", "简洁信息图", "Clean infographic", "商品主导、清晰层级与事实标注安全区。", "Product-led hierarchy with safe space for factual callouts."],
  ["seasonal_campaign", "节日营销场景", "Seasonal campaign", "克制的节日元素与商业灯光。", "Restrained seasonal accents with polished commercial light."],
].map(([key, zh, en, zhDescription, enDescription]) => ({
  key,
  name: { zh, en },
  description: { zh: zhDescription, en: enDescription },
}));

export const imageStyleKeys = imageStylePresets.map((preset) => preset.key);

export function imageStyleLabel(key: string, language: "zh" | "en") {
  return imageStylePresets.find((preset) => preset.key === key)?.name[language] ?? key;
}
