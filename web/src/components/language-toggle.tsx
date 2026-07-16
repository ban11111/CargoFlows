"use client";

import { Languages } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLanguage } from "@/lib/i18n";

export function LanguageToggle() {
  const { language, setLanguage, t } = useLanguage();
  const nextLanguage = language === "zh" ? "en" : "zh";

  return (
    <Button
      aria-label={t("language")}
      title={t("language")}
      variant="secondary"
      size="sm"
      onClick={() => setLanguage(nextLanguage)}
    >
      <Languages className="h-4 w-4" />
      {language === "zh" ? t("chinese") : t("english")}
    </Button>
  );
}

