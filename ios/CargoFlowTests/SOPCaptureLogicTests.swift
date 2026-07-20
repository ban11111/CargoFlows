import XCTest
import UIKit
@testable import CargoFlow

@MainActor
final class SOPCaptureLogicTests: XCTestCase {
    func testCaptureUploadJPEGDownsizesToConfiguredPixelLimit() throws {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true
        let source = UIGraphicsImageRenderer(size: CGSize(width: 240, height: 180), format: format).image { context in
            UIColor.systemBlue.setFill()
            context.fill(CGRect(x: 0, y: 0, width: 240, height: 180))
        }

        let data = try XCTUnwrap(captureUploadJPEGData(from: source, maxPixelDimension: 120))
        let decoded = try XCTUnwrap(UIImage(data: data))

        XCTAssertEqual(Array(data.prefix(3)), [0xff, 0xd8, 0xff])
        XCTAssertLessThanOrEqual(max(decoded.size.width * decoded.scale, decoded.size.height * decoded.scale), 120)
        XCTAssertEqual(decoded.size.width / decoded.size.height, 4.0 / 3.0, accuracy: 0.01)
    }

    func testCaptureUploadJPEGRejectsInvalidOptions() {
        XCTAssertNil(captureUploadJPEGData(from: UIImage(), maxPixelDimension: 0))
        XCTAssertNil(captureUploadJPEGData(from: UIImage(), compressionQuality: 2))
    }

    func testOptionalPackagingDoesNotBlockFinish() throws {
        let views = try [viewFixture(id: "reference", required: true), viewFixture(id: "packaging", required: false)]

        XCTAssertTrue(requiredViewsComplete(views: views, capturedViewIDs: ["reference"]))
    }

    func testEveryRequiredViewMustBeCaptured() throws {
        let views = try [
            viewFixture(id: "reference", required: true),
            viewFixture(id: "back", required: true),
            viewFixture(id: "packaging", required: false),
        ]

        XCTAssertFalse(requiredViewsComplete(views: views, capturedViewIDs: ["reference", "packaging"]))
        XCTAssertTrue(requiredViewsComplete(views: views, capturedViewIDs: ["reference", "back"]))
    }

    func testPublishedCandidatesAreFilteredAndSortedDeterministically() throws {
        let summaries = try [
            summaryFixture(sopID: "sop-b", versions: [("b-v1", 1, "published"), ("b-draft", 2, "draft")]),
            summaryFixture(sopID: "sop-a", versions: [("a-v1", 1, "published"), ("a-v3", 3, "published")]),
        ]

        XCTAssertEqual(publishedVersionCandidates(from: summaries).map(\.id), ["a-v3", "a-v1", "b-v1"])
    }

    func testChangingVersionRequiresConfirmationAndThenResetsCaptureAndSession() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
            SOPVersionCandidate(id: "v2", sopID: "sop", versionNumber: 2, name: LocalizedText(zhCN: "二", en: "Two")),
        ])
        state.recordCapture(viewID: "view-1", image: UIImage())
        _ = try await state.resolveSession { self.sessionFixture(versionID: $0) }

        XCTAssertEqual(state.requestSelection("v2"), .needsConfirmation)
        XCTAssertEqual(state.selectedVersionID, "v1")
        XCTAssertEqual(state.capturedViewIDs, ["view-1"])

        state.confirmSelection("v2")

        XCTAssertEqual(state.selectedVersionID, "v2")
        XCTAssertTrue(state.capturedViewIDs.isEmpty)
        XCTAssertNil(state.session)
    }

    func testStaleVersionLoadCannotReplaceNewerSelection() throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
            SOPVersionCandidate(id: "v2", sopID: "sop", versionNumber: 2, name: LocalizedText(zhCN: "二", en: "Two")),
        ])
        let staleToken = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.requestSelection("v2"), .changed)
        let currentToken = try XCTUnwrap(state.beginVersionLoad())

        XCTAssertEqual(state.accept(version: try versionFixture(id: "v1", number: 1), token: staleToken), .stale)
        XCTAssertEqual(state.accept(version: try versionFixture(id: "v2", number: 2), token: currentToken), .accepted)
        XCTAssertEqual(state.version?.id, "v2")
    }

    func testCurrentVersionLoadWithMismatchedResponseIDIsInvalid() throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        let token = try XCTUnwrap(state.beginVersionLoad())

        XCTAssertEqual(state.accept(version: try versionFixture(id: "wrong", number: 1), token: token), .invalidResponse)
        XCTAssertNil(state.version)
    }

    func testOlderCandidateRequestCannotOverwriteOrFinishNewerRequestState() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "existing", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "现有", en: "Existing")),
        ])
        let existingSession = try await state.resolveSession { self.sessionFixture(versionID: $0) }
        state.recordCapture(viewID: "view-1", image: UIImage())
        let old = state.beginCandidateLoad()
        let current = state.beginCandidateLoad()

        XCTAssertEqual(state.acceptCandidates(try [summaryFixture(sopID: "old", versions: [("old-v1", 1, "published")])], token: old), .stale)
        XCTAssertFalse(state.failCandidateLoad(token: old))
        XCTAssertFalse(state.finishCandidateLoad(token: old))
        XCTAssertTrue(state.isCandidateLoading)
        XCTAssertFalse(state.candidateLoadFailed)
        XCTAssertEqual(state.selectedVersionID, "existing")
        XCTAssertEqual(state.capturedViewIDs, ["view-1"])
        XCTAssertNotNil(state.session)

        XCTAssertEqual(state.acceptCandidates(
            try [summaryFixture(sopID: "sop", versions: [("existing", 1, "published"), ("new-v2", 2, "published")])],
            token: current
        ), .applied)
        XCTAssertTrue(state.finishCandidateLoad(token: current))

        XCTAssertEqual(state.acceptCandidates(try [summaryFixture(sopID: "old", versions: [("old-v1", 1, "published")])], token: old), .stale)
        XCTAssertFalse(state.failCandidateLoad(token: old))
        XCTAssertFalse(state.finishCandidateLoad(token: old))
        XCTAssertFalse(state.isCandidateLoading)
        XCTAssertFalse(state.candidateLoadFailed)
        XCTAssertEqual(state.candidates.map(\.id), ["new-v2", "existing"])
        XCTAssertEqual(state.selectedVersionID, "existing")
        XCTAssertEqual(state.capturedViewIDs, ["view-1"])
        XCTAssertEqual(state.session?.id, existingSession.id)
    }

    func testSessionIsCreatedLazilyOnceAndReused() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        var createCount = 0

        let first = try await state.resolveSession {
            createCount += 1
            return self.sessionFixture(versionID: $0)
        }
        let second = try await state.resolveSession {
            createCount += 1
            return self.sessionFixture(versionID: $0)
        }

        XCTAssertEqual(createCount, 1)
        XCTAssertEqual(first.id, second.id)
    }

    func testConcurrentFirstUploadsShareOneSessionCreation() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        var createCount = 0
        let create: @MainActor (String) async throws -> PhotoSession = { versionID in
            createCount += 1
            try await Task.sleep(for: .milliseconds(50))
            return self.sessionFixture(versionID: versionID)
        }

        async let first = state.resolveSession(create: create)
        async let second = state.resolveSession(create: create)
        _ = try await (first, second)

        XCTAssertEqual(createCount, 1)
    }

    func testResolvedSessionIsReusedAfterVersionBecomesArchived() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        let load = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.accept(version: try versionFixture(id: "v1", number: 1), token: load), .accepted)
        let original = try await state.resolveSession { self.sessionFixture(versionID: $0) }
        let archiveRefresh = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(
            state.accept(version: try versionFixture(id: "v1", number: 1, status: "archived"), token: archiveRefresh),
            .accepted
        )

        let reused = try await state.resolveSession { _ in
            XCTFail("An archived version must not trigger a new session after one already exists")
            throw APIError.server(409)
        }

        XCTAssertEqual(reused.id, original.id)
    }

    func testSession409ClassifiesAsSessionCreationFailure() {
        let failure = classifyCaptureFailure(APIError.server(409), during: .sessionCreation)

        XCTAssertEqual(failure, .session)
        XCTAssertEqual(failure.titleKey, "capture.session.failed")
        XCTAssertEqual(failure.descriptionKey, "capture.session.failed.desc")
        XCTAssertNotEqual(failure, .upload)
    }

    func testShotRowAccessibilityLabelIncludesInstructionExactlyOnce() throws {
        let view = try viewFixture(id: "reference", required: true)

        let label = shotRowAccessibilityLabel(
            view: view,
            language: .en,
            kind: "Reference Front",
            requirement: "Required"
        )

        XCTAssertTrue(label.contains("Instruction"))
        XCTAssertEqual(label.components(separatedBy: "Instruction").count - 1, 1)
        XCTAssertTrue(label.contains("01"))
    }

    func testChineseShotRowHintDescribesVoiceOverDoubleTap() {
        let language = LanguageStore()
        language.language = .zh

        XCTAssertEqual(language.t("capture.row.hint"), "轻点两下以拍摄或选择这张照片。")
    }

    func testPublishedRefreshRetainsOmittedVersionForResolvedActiveSession() async throws {
        let state = SOPCaptureState()
        let initial = state.beginCandidateLoad()
        XCTAssertEqual(
            state.acceptCandidates(try [summaryFixture(sopID: "sop", versions: [("v1", 1, "published")])], token: initial),
            .applied
        )
        _ = state.finishCandidateLoad(token: initial)
        let detail = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.accept(version: try versionFixture(id: "v1", number: 1), token: detail), .accepted)
        _ = state.finishVersionLoad(token: detail)
        state.recordCapture(viewID: "view-1", image: UIImage())
        let session = try await state.resolveSession { self.sessionFixture(versionID: $0) }

        let refresh = state.beginCandidateLoad()
        XCTAssertEqual(state.acceptCandidates([], token: refresh), .preservedActiveSession)
        _ = state.finishCandidateLoad(token: refresh)

        XCTAssertEqual(state.selectedVersionID, "v1")
        XCTAssertEqual(state.version?.id, "v1")
        XCTAssertEqual(state.capturedViewIDs, ["view-1"])
        XCTAssertNotNil(state.capturedImages["view-1"])
        XCTAssertEqual(state.session?.id, session.id)
        XCTAssertEqual(state.candidates.map(\.id), ["v1"])
        XCTAssertFalse(state.candidates[0].availableForNewSession)
        let reused = try await state.resolveSession { _ in
            XCTFail("Omitted active version must reuse its resolved session")
            throw APIError.server(409)
        }
        XCTAssertEqual(reused.id, session.id)
    }

    func testOmittedVersionWithoutSessionConfirmsThenResetsAllCaptureState() throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        let detail = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.accept(version: try versionFixture(id: "v1", number: 1), token: detail), .accepted)
        state.recordCapture(viewID: "view-1", image: UIImage())

        let refresh = state.beginCandidateLoad()
        XCTAssertEqual(
            state.acceptCandidates(try [summaryFixture(sopID: "sop", versions: [("v2", 2, "published")])], token: refresh),
            .needsConfirmation
        )
        XCTAssertEqual(state.selectedVersionID, "v1")
        XCTAssertNotNil(state.capturedImages["view-1"])
        XCTAssertFalse(state.candidates.first(where: { $0.id == "v1" })?.availableForNewSession ?? true)

        state.confirmCandidateTransition()

        XCTAssertEqual(state.selectedVersionID, "v2")
        XCTAssertNil(state.version)
        XCTAssertTrue(state.capturedViewIDs.isEmpty)
        XCTAssertTrue(state.capturedImages.isEmpty)
        XCTAssertNil(state.session)
    }

    func testSingleViewReplacesWhileMultipleViewAppends() {
        let state = SOPCaptureState()
        let first = UIImage()
        let second = UIImage()

        state.recordCapture(viewID: "single", image: first)
        state.recordCapture(viewID: "single", image: second)
        state.recordCapture(viewID: "supplemental", image: first, allowMultiple: true)
        state.recordCapture(viewID: "supplemental", image: second, allowMultiple: true)

        XCTAssertEqual(state.capturedImageLists["single"]?.count, 1)
        XCTAssertEqual(state.capturedImageLists["supplemental"]?.count, 2)
        XCTAssertEqual(state.capturedViewIDs, ["single", "supplemental"])
    }

    func testOlderVersionRequestCannotChangeNewerLoadingOrErrorState() throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
            SOPVersionCandidate(id: "v2", sopID: "sop", versionNumber: 2, name: LocalizedText(zhCN: "二", en: "Two")),
        ])
        let old = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.requestSelection("v2"), .changed)
        let current = try XCTUnwrap(state.beginVersionLoad())

        XCTAssertEqual(state.accept(version: try versionFixture(id: "v1", number: 1), token: old), .stale)
        XCTAssertFalse(state.failVersionLoad(token: old))
        XCTAssertFalse(state.finishVersionLoad(token: old))
        XCTAssertTrue(state.isVersionLoading)
        XCTAssertFalse(state.versionLoadFailed)
        XCTAssertEqual(state.selectedVersionID, "v2")

        XCTAssertEqual(state.accept(version: try versionFixture(id: "v2", number: 2), token: current), .accepted)
        XCTAssertTrue(state.finishVersionLoad(token: current))
        XCTAssertFalse(state.isVersionLoading)
        XCTAssertFalse(state.versionLoadFailed)

        XCTAssertFalse(state.failVersionLoad(token: old))
        XCTAssertFalse(state.finishVersionLoad(token: old))
        XCTAssertEqual(state.version?.id, "v2")
        XCTAssertFalse(state.versionLoadFailed)
    }

    private func viewFixture(id: String, required: Bool) throws -> SOPView {
        let data = Data(#"{"public_id":"\#(id)","sequence":1,"role":"capture","view_kind":"standard","preset_key":null,"name":{"zh-CN":"视图","en":"View"},"instruction":{"zh-CN":"说明","en":"Instruction"},"required":\#(required),"pose":{"space":"object","camera_position_direction":[0,0,1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.85,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false},"reference_images":[]}"#.utf8)
        return try JSONDecoder().decode(SOPView.self, from: data)
    }

    private func summaryFixture(sopID: String, versions: [(String, Int, String)]) throws -> CaptureSOPSummary {
        let versionPayload = versions.map { id, number, status in
            #"{"schema_version":"1.0","public_id":"\#(id)","sop_public_id":"\#(sopID)","version_number":\#(number),"status":"\#(status)","name":{"zh-CN":"规范 \#(number)","en":"SOP \#(number)"},"description":{"zh-CN":"","en":""},"coordinate_system":{"id":"pcs_object_v1","handedness":"right_handed","origin":"bounding_box_center","unit":"normalized","axes":{"x_positive":"object_top","y_positive":"object_left","z_positive":"object_front"}},"published_at":null,"created_at":null,"updated_at":null,"views":[]}"#
        }.joined(separator: ",")
        let data = Data(#"{"public_id":"\#(sopID)","category_id":1,"versions":[\#(versionPayload)],"created_at":null,"updated_at":null}"#.utf8)
        return try JSONDecoder().decode(CaptureSOPSummary.self, from: data)
    }

    private func versionFixture(id: String, number: Int, status: String = "published") throws -> SOPVersion {
        try summaryFixture(sopID: "sop", versions: [(id, number, status)]).versions[0]
    }

    private func sessionFixture(versionID: String) -> PhotoSession {
        PhotoSession(publicID: "session", code: "PS-1", skuID: "sku", sopVersionID: versionID, status: "in_progress", createdAt: Date(timeIntervalSince1970: 0))
    }
}
