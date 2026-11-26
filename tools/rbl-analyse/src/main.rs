// SPDX-License-Identifier: GPL-2.0-or-later
// Decode/analysis pipeline adapted from RBxport 1.10.0 src-tauri/src/analysis.rs.
use serde_json::{json, Value};
use std::{error::Error, path::PathBuf};

const REV: &str = "d0ff75b378ec85be891d9f3067ada1e82bad4cae";

#[derive(Debug, PartialEq)]
struct Args {
    audio: PathBuf,
    mode: String,
    out: PathBuf,
    existing: Option<PathBuf>,
    notation: String,
}

fn parse(args: impl IntoIterator<Item = String>) -> Result<Args, Box<dyn Error>> {
    let mut audio = None;
    let mut mode = None;
    let mut out = None;
    let mut existing = None;
    let mut notation = "classic".to_owned();
    let mut args = args.into_iter();
    while let Some(flag) = args.next() {
        let value = args
            .next()
            .ok_or_else(|| format!("missing value for {flag}"))?;
        match flag.as_str() {
            "--audio" => audio = Some(PathBuf::from(value)),
            "--mode" => mode = Some(value),
            "--out" => out = Some(PathBuf::from(value)),
            "--existing" => existing = Some(PathBuf::from(value)),
            "--key-notation" => notation = value,
            _ => return Err(format!("unknown argument {flag}").into()),
        }
    }
    let mode = mode.ok_or("--mode rbxport|rekordbox is required")?;
    if !matches!(mode.as_str(), "rbxport" | "rekordbox") {
        return Err("--mode must be rbxport or rekordbox".into());
    }
    if !matches!(notation.as_str(), "classic" | "alphanumeric") {
        return Err("--key-notation must be classic or alphanumeric".into());
    }
    Ok(Args {
        audio: audio.ok_or("--audio is required")?,
        mode,
        out: out.ok_or("--out is required")?,
        existing,
        notation,
    })
}

fn report(args: &Args, analysis: &rbl_analysis::Analysis, duration: f64) -> Value {
    let camelot = analysis.key.as_ref().map(|k| k.camelot());
    let key = analysis.key.as_ref().map(|k| {
        if args.notation == "alphanumeric" {
            k.camelot()
        } else {
            k.name.clone()
        }
    });
    json!({"bpm": analysis.tempo.bpm, "key": key, "camelot": camelot,
        "beat_count": analysis.tempo.beats.len(),
        "first_beat_ms": analysis.tempo.beats.first().map(|b| b.time_ms),
        "tempo_segment_count": analysis.tempo.segments.len(), "duration_seconds": duration,
        "files": ["ANLZ0000.DAT", "ANLZ0000.EXT", "ANLZ0000.2EX"],
        "mode": args.mode, "analyser_version": REV})
}

fn run(args: Args) -> Result<Value, Box<dyn Error>> {
    let preset = if args.mode == "rbxport" {
        rbl_analysis::AnalysisPreset::Rbxport
    } else {
        rbl_analysis::AnalysisPreset::Rekordbox
    };
    let mut options = preset.options();
    // RBxport's AnalysisSettings defaults apply these to both presets.
    options.tempo.min_bpm = 70.0;
    options.tempo.max_bpm = 180.0;
    options.tempo.placement = rbl_analysis::tempo::Placement::Attack;
    let mut stream = rbl_audio::MonoStream::open(&args.audio)?;
    let rate = stream.sample_rate();
    let cap = 1800 * rate as usize;
    let mut creator = rbl_analysis::wave_creator::WaveCreator::new(rate);
    if let Some(frames) = stream.expected_frames() {
        creator.reserve(frames);
    }
    let mut head = Vec::with_capacity(cap.min(1 << 24));
    let mut builder: Option<rbl_analysis::waveform::Builder> = None;
    let mut past_head = false;
    while let Some((mono, stereo)) = stream.next_frames() {
        creator.push(stereo);
        if past_head {
            builder.as_mut().unwrap().push(mono);
        } else {
            head.extend_from_slice(mono);
            if head.len() > cap {
                let mut full = rbl_analysis::waveform::Builder::new(rate);
                full.push(&head);
                builder = Some(full);
                head.truncate(cap);
                head.shrink_to_fit();
                past_head = true;
            }
        }
    }
    if head.is_empty() {
        return Err("decoded no samples".into());
    }
    let mut analysis = rbl_analysis::analyse_with(&head, rate, options);
    let duration = if let Some(builder) = builder {
        let duration = builder.samples() as f64 / rate.max(1) as f64;
        analysis.peak = analysis.peak.max(builder.peak());
        analysis.tempo.extend_to(duration);
        analysis.waveform = builder.finish();
        duration
    } else {
        head.len() as f64 / rate as f64
    };
    let w = creator.finish();
    let waves = rbl_anlz::RekordboxWaves {
        pwav: w.pwav,
        pwv2: w.pwv2,
        pwvc: w.pwvc,
        pwv3: w.pwv3,
        pwv4: w.pwv4,
        pwv5: w.pwv5,
        pwv6: w.pwv6,
        pwv7: w.pwv7,
    };
    let beats: Vec<_> = analysis
        .tempo
        .beats
        .iter()
        .map(|b| rbl_anlz::Beat {
            beat_number: b.beat_number,
            tempo_x100: b.tempo_x100,
            time_ms: b.time_ms,
        })
        .collect();
    // RBxport ignores missing/unparseable existing siblings independently.
    let existing: Vec<_> = ["DAT", "EXT", "2EX"]
        .iter()
        .map(|ext| {
            args.existing
                .as_ref()
                .and_then(|p| rbl_anlz::Anlz::read(&p.join(format!("ANLZ0000.{ext}"))).ok())
        })
        .collect();
    let files = rbl_anlz::author_with_rekordbox_waves(
        &rbl_anlz::share_tree_ppth(&args.audio.to_string_lossy()),
        &beats,
        &waves,
        rbl_anlz::Existing {
            dat: existing[0].as_ref(),
            ext: existing[1].as_ref(),
            two_ex: existing[2].as_ref(),
        },
    );
    std::fs::create_dir_all(&args.out)?;
    for ext in ["DAT", "EXT", "2EX"] {
        if args.out.join(format!("ANLZ0000.{ext}")).exists() {
            return Err("output already contains analysis files; use an empty directory".into());
        }
    }
    for (ext, bytes) in [
        ("DAT", files.dat),
        ("EXT", files.ext),
        ("2EX", files.two_ex),
    ] {
        std::fs::write(args.out.join(format!("ANLZ0000.{ext}")), bytes)?;
    }
    Ok(report(&args, &analysis, duration))
}

fn main() {
    match parse(std::env::args().skip(1)).and_then(run) {
        Ok(value) => println!("{value}"),
        Err(error) => {
            eprintln!("{}", json!({"error":error.to_string()}));
            std::process::exit(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn args(extra: &[&str]) -> Vec<String> {
        [
            "--audio",
            "/tmp/a.wav",
            "--mode",
            "rbxport",
            "--out",
            "/tmp/out",
        ]
        .iter()
        .chain(extra)
        .map(|s| s.to_string())
        .collect()
    }
    #[test]
    fn arguments() {
        assert_eq!(parse(args(&[])).unwrap().notation, "classic");
        assert_eq!(
            parse(args(&["--key-notation", "alphanumeric"]))
                .unwrap()
                .notation,
            "alphanumeric"
        );
        for extra in [
            &["--mode", "wrong"][..],
            &["--key-notation", "wrong"],
            &["--unknown", "x"],
            &["--audio"],
        ] {
            assert!(parse(args(extra)).is_err());
        }
        assert!(parse(Vec::<String>::new()).is_err());
    }
    #[test]
    fn json_contract() {
        let args = parse(args(&[])).unwrap();
        let analysis = rbl_analysis::analyse_with(
            &vec![0.0; 4410],
            44100,
            rbl_analysis::AnalysisPreset::Rbxport.options(),
        );
        let value = report(&args, &analysis, 0.1);
        for key in [
            "bpm",
            "key",
            "camelot",
            "beat_count",
            "first_beat_ms",
            "tempo_segment_count",
            "duration_seconds",
            "files",
            "mode",
            "analyser_version",
        ] {
            assert!(value.get(key).is_some(), "missing {key}");
        }
        assert_eq!(value["files"].as_array().unwrap().len(), 3);
        assert_eq!(value["analyser_version"], REV);
    }
}
