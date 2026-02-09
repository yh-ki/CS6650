#!/usr/bin/env python3

import json
import re
from collections import Counter
import argparse
import sys

def count_words_locally(text_file: str) -> dict:
    """Count words using standard Python approach"""
    word_regex = re.compile(r'[a-zA-Z]+')
    word_counts = Counter()
    
    with open(text_file, 'r') as f:
        content = f.read()
        words = word_regex.findall(content.lower())
        word_counts.update(words)
    
    return dict(word_counts)

def load_mapreduce_results(json_file: str) -> dict:
    """Load results from MapReduce JSON output"""
    with open(json_file, 'r') as f:
        return json.load(f)

def compare_results(local: dict, mapreduce: dict) -> tuple:
    """Compare local and MapReduce results"""
    all_words = set(local.keys()) | set(mapreduce.keys())
    
    differences = []
    matches = 0
    
    for word in sorted(all_words):
        local_count = local.get(word, 0)
        mr_count = mapreduce.get(word, 0)
        
        if local_count != mr_count:
            differences.append({
                'word': word,
                'local': local_count,
                'mapreduce': mr_count,
                'diff': abs(local_count - mr_count)
            })
        else:
            matches += 1
    
    return matches, differences

def print_results(local: dict, mapreduce: dict, matches: int, differences: list):
    """Print comparison results"""
    print("=" * 70)
    print("MapReduce Verification Results")
    print("=" * 70)
    print()
    
    print(f"Total unique words (local):     {len(local):,}")
    print(f"Total unique words (MapReduce): {len(mapreduce):,}")
    print(f"Total word occurrences (local):     {sum(local.values()):,}")
    print(f"Total word occurrences (MapReduce): {sum(mapreduce.values()):,}")
    print()
    
    print(f"Matching words: {matches:,}")
    print(f"Differences:    {len(differences):,}")
    print()
    
    if differences:
        print("⚠️  VERIFICATION FAILED - Differences found:")
        print()
        print(f"{'Word':<20} {'Local':<10} {'MapReduce':<10} {'Difference':<10}")
        print("-" * 70)
        for diff in differences[:20]:  # Show first 20 differences
            print(f"{diff['word']:<20} {diff['local']:<10} {diff['mapreduce']:<10} {diff['diff']:<10}")
        
        if len(differences) > 20:
            print(f"... and {len(differences) - 20} more differences")
        
        return False
    else:
        print("✓ VERIFICATION PASSED - All word counts match!")
        print()
        print("Top 10 most common words:")
        print(f"{'Rank':<6} {'Word':<20} {'Count':<10}")
        print("-" * 40)
        
        sorted_words = sorted(local.items(), key=lambda x: x[1], reverse=True)
        for i, (word, count) in enumerate(sorted_words[:10], 1):
            print(f"{i:<6} {word:<20} {count:<10,}")
        
        return True

def main():
    parser = argparse.ArgumentParser(description='Verify MapReduce word count results')
    parser.add_argument('--input', required=True, help='Original input text file')
    parser.add_argument('--output', required=True, help='MapReduce output JSON file')
    
    args = parser.parse_args()
    
    print("Computing local word counts...")
    local_counts = count_words_locally(args.input)
    
    print("Loading MapReduce results...")
    mr_counts = load_mapreduce_results(args.output)
    
    print("Comparing results...")
    print()
    
    matches, differences = compare_results(local_counts, mr_counts)
    success = print_results(local_counts, mr_counts, matches, differences)
    
    return 0 if success else 1

if __name__ == '__main__':
    sys.exit(main())
