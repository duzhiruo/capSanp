import AppKit
import Foundation
import Vision

struct OCRLine: Encodable {
    let text: String
    let confidence: Float
}

struct OCRResult: Encodable {
    let text: String
    let lines: [OCRLine]
}

enum OCRError: Error {
    case missingPath
    case imageLoadFailed
    case cgImageMissing
}

func recognizeText(path: String) throws -> OCRResult {
    guard let image = NSImage(contentsOfFile: path) else {
        throw OCRError.imageLoadFailed
    }
    guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
        throw OCRError.cgImageMissing
    }

    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = true
    request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US"]

    let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
    try handler.perform([request])

    let observations = request.results ?? []
    let lines = observations.compactMap { observation -> OCRLine? in
        guard let candidate = observation.topCandidates(1).first else {
            return nil
        }
        return OCRLine(text: candidate.string, confidence: candidate.confidence)
    }
    return OCRResult(text: lines.map(\.text).joined(separator: "\n"), lines: lines)
}

do {
    guard CommandLine.arguments.count >= 2 else {
        throw OCRError.missingPath
    }
    let result = try recognizeText(path: CommandLine.arguments[1])
    let data = try JSONEncoder().encode(result)
    FileHandle.standardOutput.write(data)
} catch {
    let message = "{\"error\":\"\(String(describing: error))\"}"
    FileHandle.standardError.write(Data(message.utf8))
    exit(1)
}
