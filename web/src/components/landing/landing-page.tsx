"use client";

import {
  Apple,
  ArrowUpRight,
  Camera,
  Check,
  ClipboardCheck,
  Cloud,
  Download,
  PackageCheck,
  ScanLine,
} from "lucide-react";
import Link from "next/link";
import { LanguageToggle } from "@/components/language-toggle";
import { useLanguage } from "@/lib/i18n";
import styles from "./landing-page.module.css";

const downloadUrl = "/api/downloads/ios";

export function LandingPage({ publicAppUrl }: { publicAppUrl: string }) {
  const { t } = useLanguage();

  return (
    <div className={styles.page}>
      <a className={styles.skipLink} href="#main-content">
        {t("landingSkipToContent")}
      </a>

      <header className={styles.header}>
        <Link className={styles.brand} href="/" aria-label={t("landingHomeLabel")}>
          <span className={styles.brandMark} aria-hidden="true">
            <span />
            <span />
            <span />
          </span>
          <span>CargoFlow</span>
        </Link>
        <nav className={styles.nav} aria-label={t("landingNavigationLabel")}>
          <Link className={styles.adminLink} href="/login">
            {t("landingAdmin")}
            <ArrowUpRight aria-hidden="true" size={15} />
          </Link>
          <LanguageToggle />
        </nav>
      </header>

      <main id="main-content">
        <section className={styles.hero} aria-labelledby="landing-title">
          <div className={styles.heroCopy}>
            <p className={styles.eyebrow}>
              <Apple aria-hidden="true" size={16} fill="currentColor" />
              {t("landingEyebrow")}
            </p>
            <h1 id="landing-title">{t("landingTitle")}</h1>
            <p className={styles.lede}>{t("landingDescription")}</p>
            <div className={styles.heroActions}>
              <a className={styles.downloadButton} href={downloadUrl}>
                <Download aria-hidden="true" size={20} />
                <span>
                  <strong>{t("landingDownload")}</strong>
                  <small>{t("landingDownloadMeta")}</small>
                </span>
              </a>
              <p className={styles.requirement}>{t("landingRequirement")}</p>
            </div>
          </div>

          <div className={styles.productStage} aria-label={t("landingPreviewLabel")}>
            <div className={styles.stageGlow} />
            <div className={styles.phone}>
              <div className={styles.phoneTop} aria-hidden="true" />
              <div className={styles.phoneScreen}>
                <div className={styles.statusBar}>
                  <span>9:41</span>
                  <span className={styles.statusIcons}>● ●</span>
                </div>
                <div className={styles.captureHeader}>
                  <div>
                    <span>{t("landingMockSession")}</span>
                    <strong>{t("landingMockProduct")}</strong>
                  </div>
                  <span className={styles.progressPill}>3 / 6</span>
                </div>
                <div className={styles.viewfinder}>
                  <div className={styles.focusFrame}>
                    <span className={styles.cornerOne} />
                    <span className={styles.cornerTwo} />
                    <span className={styles.cornerThree} />
                    <span className={styles.cornerFour} />
                    <div className={styles.packageShape}>
                      <div className={styles.packageLabel}>CF</div>
                    </div>
                  </div>
                  <div className={styles.cameraGuide}>
                    <ScanLine aria-hidden="true" size={16} />
                    {t("landingMockGuide")}
                  </div>
                </div>
                <div className={styles.captureRail}>
                  <div className={styles.railItemDone}>
                    <Check aria-hidden="true" size={13} />
                    <span>{t("landingMockFront")}</span>
                  </div>
                  <div className={styles.railItemActive}>
                    <Camera aria-hidden="true" size={14} />
                    <span>{t("landingMockSide")}</span>
                  </div>
                  <div className={styles.railItemPending}>
                    <span />
                    <span>{t("landingMockDetail")}</span>
                  </div>
                </div>
                <button className={styles.shutter} type="button" aria-label={t("landingMockCapture")}>
                  <span />
                </button>
              </div>
            </div>
            <p className={styles.stageCaption}>{t("landingPreviewCaption")}</p>
          </div>
        </section>

        <section className={styles.workflow} aria-labelledby="workflow-title">
          <div className={styles.sectionIntro}>
            <p className={styles.sectionLabel}>{t("landingWorkflowLabel")}</p>
            <h2 id="workflow-title">{t("landingWorkflowTitle")}</h2>
          </div>
          <div className={styles.featureGrid}>
            <article className={styles.featureCard}>
              <PackageCheck aria-hidden="true" />
              <h3>{t("landingFeatureSkuTitle")}</h3>
              <p>{t("landingFeatureSkuDescription")}</p>
            </article>
            <article className={styles.featureCard}>
              <ClipboardCheck aria-hidden="true" />
              <h3>{t("landingFeatureSopTitle")}</h3>
              <p>{t("landingFeatureSopDescription")}</p>
            </article>
            <article className={styles.featureCard}>
              <Camera aria-hidden="true" />
              <h3>{t("landingFeatureCaptureTitle")}</h3>
              <p>{t("landingFeatureCaptureDescription")}</p>
            </article>
          </div>
        </section>

        <section className={styles.downloadPanel} aria-labelledby="download-title">
          <div className={styles.downloadCopy}>
            <p className={styles.sectionLabel}>{t("landingDownloadLabel")}</p>
            <h2 id="download-title">{t("landingDownloadTitle")}</h2>
            <p>{t("landingDownloadDescription")}</p>
          </div>
          <div className={styles.downloadAside}>
            <a className={styles.downloadButton} href={downloadUrl}>
              <Download aria-hidden="true" size={20} />
              <span>
                <strong>{t("landingDownload")}</strong>
                <small>{t("landingDownloadMeta")}</small>
              </span>
            </a>
            <p className={styles.packageNote}>{t("landingPackageNote")}</p>
          </div>
        </section>
      </main>

      <footer className={styles.footer}>
        <p>© 2026 CargoFlow</p>
        <a href={publicAppUrl} target="_blank" rel="noreferrer">
          <Cloud aria-hidden="true" size={15} />
          {t("landingCloudflareConnected")}
        </a>
      </footer>
    </div>
  );
}
