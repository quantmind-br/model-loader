#!/usr/bin/env python3
"""Emit the curated SummaryBench dataset as JSON.

Synthetic, authored for the model-loader multi-document summarization-coherence
benchmark: 5 bundles, each with 10 short documents and 5 key facts a faithful
summary must mention. A grader scores coherence + fact coverage.

Reproduce:  python3 tools/summary-curate/main.py > \
    internal/service/benchmark/data/summary_curated.json
"""
import json
import sys

# Each bundle: (id, [10 docs], [5 key facts verbatim in the docs])
BUNDLES = [
    ("sum-01",
     ["The Merrowfield wind farm opened in 2028.",
      "It has 64 turbines arranged in four rows.",
      "Peak output is 410 megawatts.",
      "The site spans 90 square kilometers of moorland.",
      "Local birds were tracked for two years before approval.",
      "Maintenance is handled by a crew of 38 technicians.",
      "Power feeds three neighboring counties.",
      "The project cost 1.2 billion in public-private funding.",
      "A visitor center opened alongside the farm.",
      "Turbine blades are recycled at end of life."],
     ["The Merrowfield wind farm opened in 2028.",
      "It has 64 turbines arranged in four rows.",
      "Peak output is 410 megawatts.",
      "The project cost 1.2 billion in public-private funding.",
      "Power feeds three neighboring counties."]),
    ("sum-02",
     ["The Lirian Library holds 2.4 million volumes.",
      "It was founded in 1887 by a merchant guild.",
      "The reading room seats 300 scholars.",
      "A rare-manuscript wing was added in 1954.",
      "Annual visitors number around 500,000.",
      "Digitization began in 2010.",
      "The building survived a major fire in 1921.",
      "Membership is free to city residents.",
      "It houses a famous medieval atlas.",
      "The library employs 120 staff."],
     ["The Lirian Library holds 2.4 million volumes.",
      "It was founded in 1887 by a merchant guild.",
      "The building survived a major fire in 1921.",
      "Annual visitors number around 500,000.",
      "It houses a famous medieval atlas."]),
    ("sum-03",
     ["The Cassine River is 612 kilometers long.",
      "It flows through three countries.",
      "Its delta supports a large wetland reserve.",
      "Seasonal floods deposit fertile silt.",
      "Twelve bridges cross the river.",
      "A hydroelectric dam was built in 1976.",
      "The river is home to an endemic catfish.",
      "Cargo barges use the lower 200 kilometers.",
      "Water quality has improved since 2005.",
      "The source is a glacial lake."],
     ["The Cassine River is 612 kilometers long.",
      "It flows through three countries.",
      "A hydroelectric dam was built in 1976.",
      "The river is home to an endemic catfish.",
      "The source is a glacial lake."]),
    ("sum-04",
     ["The Orsel marathon attracts 45,000 runners.",
      "The course passes seven historic landmarks.",
      "It has been held annually since 1981.",
      "The record time is 2 hours and 4 minutes.",
      "Proceeds fund local youth sports.",
      "Water stations appear every 5 kilometers.",
      "The route is closed to traffic for 8 hours.",
      "Elite runners come from over 30 countries.",
      "A wheelchair division started in 1995.",
      "The finish line is in the central plaza."],
     ["The Orsel marathon attracts 45,000 runners.",
      "It has been held annually since 1981.",
      "The record time is 2 hours and 4 minutes.",
      "Proceeds fund local youth sports.",
      "A wheelchair division started in 1995."]),
    ("sum-05",
     ["The Veld telescope array has 36 dishes.",
      "Each dish is 25 meters wide.",
      "It studies radio emissions from distant galaxies.",
      "The array sits on a high desert plateau.",
      "Construction finished in 2022.",
      "Data is processed at an on-site supercomputer.",
      "It operates in partnership with six universities.",
      "Observing time is allocated by peer review.",
      "The site has near-zero radio interference.",
      "A future expansion to 60 dishes is planned."],
     ["The Veld telescope array has 36 dishes.",
      "Each dish is 25 meters wide.",
      "Construction finished in 2022.",
      "It operates in partnership with six universities.",
      "A future expansion to 60 dishes is planned."]),
]


def main():
    items = [{"id": bid, "documents": docs, "facts": facts} for bid, docs, facts in BUNDLES]
    json.dump(items, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
